package server

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/jeffreylean/goraft/client"
	raft "github.com/jeffreylean/goraft/proto"
)

type Role int

const (
	Leader Role = iota
	Follower
	Candidate
	Init
)

func (r Role) String() string {
	names := [...]string{
		"Leader",
		"Follower",
		"Candidate",
		"Inir",
	}

	return names[r]
}

type Event int

const (
	Election Event = iota
	WinElection
	LostElection
	Initialize
)

type State struct {
	CurrentRole     Role
	roleChan        chan Role
	timesupChan     chan bool
	healthCheckChan chan bool
}

func (s *Server) InitializeState(ctx context.Context) {
	state := &State{
		roleChan:        make(chan Role),
		healthCheckChan: make(chan bool),
		timesupChan:     make(chan bool),
	}
	s.State = state

	go func(ctx context.Context) {
		for {
			select {
			case role := <-s.State.roleChan:
				switch role {
				case Leader:
					log.Println("Role changed to leader")
					healthCheckInterval := time.Duration(time.Microsecond * 50)
					// Create grpc Client
					c, err := client.Dial(s.cluster.Routes)
					if err != nil {
						log.Printf("Err create grpc client: %v", err)
					}
					for {
						select {
						case <-ctx.Done():
							fmt.Println("Close conn")
							for conn := range c.ServiceConn {
								conn.Close()
							}
						default:
							// Send healthCheck every 50 ms
							time.Sleep(healthCheckInterval)
							c.AppendEntries(ctx, int64(s.CurrentTerm), int64(s.cluster.ID), 0, 0, []*raft.LogEntry{})
						}
					}

				case Follower, Init:
					// Need to keep track of time
					s.State.StartTimer()
					log.Println("Role changed to follower")
				loop:
					for {
						select {
						case <-s.State.timesupChan:
							go s.State.HandleEvent(Election)
							break loop
						case <-s.State.healthCheckChan:
							fmt.Println("reset timer!!")
							s.State.StartTimer()
						}
					}
				case Candidate:
					// crate connections to all the nodes within the cluster
					log.Printf("server: Creating connection with %v", s.cluster.Routes)
					// Create grpc Client
					c, err := client.Dial(s.cluster.Routes)
					defer func() {
						fmt.Println("Close conn")
						for conn := range c.ServiceConn {
							conn.Close()
						}
					}()
					if err != nil {
						log.Printf("Err create grpc client: %v", err)
					}
					log.Println("Role changed to candidate")
					log.Println("Starting election...")
					// Request for vote
					// Increate current term first
					s.CurrentTerm++
					var lastLogIndex int64
					var lastLogTerm int64
					if len(s.Logs) > 0 {
						logEntry := s.Logs[len(s.Logs)-1]
						lastLogIndex = int64(logEntry.Index)
						lastLogTerm = int64(logEntry.Term)
					}
					latestTerm, success, err := c.RequestVote(ctx, int64(s.CurrentTerm), int64(s.cluster.ID), lastLogIndex, lastLogTerm)
					if err != nil {
						log.Printf("Err from request Vote: %v", err)
						continue
					}
					s.CurrentTerm = int(latestTerm)
					if success {
						// Win election and turn the state to Leader
						go s.State.HandleEvent(WinElection)
					} else {
						// Lost election and turn the state to Follower
						go s.State.HandleEvent(LostElection)
					}
					log.Printf("Server %d is now a %s", s.cluster.ID, s.State.CurrentRole.String())
				}
			case <-ctx.Done():
				return
			}
		}
	}(ctx)
	// Initialize the state
	s.State.HandleEvent(Initialize)
}

func (s *State) HandleEvent(e Event) {
	switch e {
	case Election:
		s.CurrentRole = Candidate
		s.roleChan <- s.CurrentRole
	case WinElection:
		s.CurrentRole = Leader
		s.roleChan <- s.CurrentRole
	case LostElection:
		s.CurrentRole = Follower
		s.roleChan <- s.CurrentRole
	default:
		s.CurrentRole = Init
		s.roleChan <- s.CurrentRole
	}
}

func (s *State) GetCurrentRole() Role {
	return s.CurrentRole
}

func (s *State) StartTimer() {
	go func() {
		duration := rand.Intn(300-150) + 150
		time.Sleep(time.Microsecond * time.Duration(duration))
		s.timesupChan <- true
	}()
}
