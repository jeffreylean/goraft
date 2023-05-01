package server

import (
	"context"
	"log"
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
				if time.Now().After(s.healthCheckTimeOut) {
					s.HealthCheckTimer()
					s.client.AppendEntries(ctx, int64(s.CurrentTerm), int64(s.ID), 0, 0, []*raft.LogEntry{})
				}
			case Follower, Init:
				// Give some buffer time for the leader to connect to this node if there is any
				time.Sleep(time.Second * 5)
				// If current time passed the healthcheck timeout, start an election
				if time.Now().After(s.electionTimeOut) {
					s.HandleEvent(Election)
					continue
				}
			case Candidate:
				// Request for vote
				// Increate current term first
				s.mu.Lock()
				s.CurrentTerm++
				s.mu.Unlock()
				var lastLogIndex int64
				var lastLogTerm int64
				if len(s.Logs) > 0 {
					logEntry := s.Logs[len(s.Logs)-1]
					lastLogIndex = int64(logEntry.Index)
					lastLogTerm = int64(logEntry.Term)
				}
				latestTerm, success, err := s.client.RequestVote(ctx, int64(s.CurrentTerm), int64(s.ID), lastLogIndex, lastLogTerm)
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

func (s *State) GetCurrentRole() Role {
	return s.CurrentRole
}
