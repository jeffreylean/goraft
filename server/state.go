package server

import (
	"context"
	"log"
	"math/rand"
	"time"

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
		"Init",
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
	CurrentRole Role
}

func (s *Server) InitializeState(ctx context.Context) {
	s.State = &State{CurrentRole: Init}

	go func(ctx context.Context) {
		ticker := time.NewTicker(time.Millisecond)
		for range ticker.C {
			switch s.State.CurrentRole {
			case Leader:
				s.mu.Lock()
				healthCheckInterval := time.Duration(time.Millisecond * 50)
				if time.Now().After(s.healthCheckTimeOut) {
					s.healthCheckTimeOut = time.Now().Add(healthCheckInterval)
					s.client.AppendEntries(ctx, int64(s.CurrentTerm), int64(s.cluster.ID), 0, 0, []*raft.LogEntry{})
				}
				s.mu.Unlock()
			case Follower, Init:
				// Give some buffer time for the leader to connect to this node if there is any
				time.Sleep(time.Second * 5)
				// If current time passed the healthcheck timeout, start an election
				if time.Now().After(s.healthCheckTimeOut) {
					s.HandleEvent(Election)
					continue
				}
			case Candidate:
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
				latestTerm, success, err := s.client.RequestVote(ctx, int64(s.CurrentTerm), int64(s.cluster.ID), lastLogIndex, lastLogTerm)
				if err != nil {
					log.Printf("Err from request Vote: %v", err)
					continue
				}
				s.CurrentTerm = int(latestTerm)
				if success {
					// Win election and turn the state to Leader
					s.HandleEvent(WinElection)
				} else {
					// Lost election and turn the state to Follower
					s.HandleEvent(LostElection)
				}
			}
		}
	}(ctx)
}

func (s *Server) HandleEvent(e Event) {
	s.mu.Lock()
	switch e {
	case Election:
		s.State.CurrentRole = Candidate
	case WinElection:
		s.State.CurrentRole = Leader
	case LostElection:
		s.State.CurrentRole = Follower
	default:
		s.State.CurrentRole = Init
	}
	s.mu.Unlock()
	log.Printf("Server is now a %s", s.State.CurrentRole.String())
}

func (s *State) GetCurrentRole() Role {
	return s.CurrentRole
}

func (s *Server) HealthCheckTimer() {
	duration := rand.Intn(300-150) + 150
	s.mu.Lock()
	s.healthCheckTimeOut = time.Now().Add(time.Millisecond * time.Duration(duration))
	s.mu.Unlock()
}
