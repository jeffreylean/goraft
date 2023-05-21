package server

import (
	"context"
	"fmt"
	"log"

	raft "github.com/jeffreylean/goraft/proto"
)

func (s *Server) AppendEntries(ctx context.Context, request *raft.AppendEntriesRequest) (*raft.AppendEntriesResponse, error) {
	resp := raft.AppendEntriesResponse{}
	resp.Term = int64(s.CurrentTerm)
	// If the leader's term is smaller than currentTerm of this nodes, return false
	if request.Term >= int64(s.CurrentTerm) {
		if request.Term == int64(s.CurrentTerm) && s.State.GetCurrentRole() == Candidate {
			s.HandleEvent(LostElection)
		} else {
			s.mu.Lock()
			// Updates current term to match the leader's
			s.State.CurrentRole = Follower
			s.VotedFor = 0
			s.CurrentTerm = uint64(request.Term)
			s.mu.Unlock()
		}
		resp.Term = int64(s.CurrentTerm)
	} else {
		return &resp, nil
	}
	// Up till this point, it should be a valid leader, so update election timeout
	s.ElectionTimer()

	// Its healthCheck
	if len(request.Entries) == 0 {
		fmt.Println("Received healthcheck from server", request.LeaderId)
		resp.Success = true
		return &resp, nil
	}

	s.mu.Lock()
	// Check previous log entry matches or not
	if len(s.Logs) > int(request.PrevLogIndex) || request.PrevLogIndex == 0 {
		if s.Logs[request.PrevLogIndex].Term == int(request.PrevLogterm) {
			resp.Success = true
		} else {
			// If the log entry is the same index, but different term, delete the existing entry and all that follow it
			filteredLogs := s.Logs[:request.PrevLogIndex+1]
			s.Logs = filteredLogs
			return &resp, nil
		}
	} else {
		return &resp, nil
	}
	// Replicate Logs
	s.Logs = append(s.Logs, parseLog(request.Entries)...)
	// Update commit index
	if request.LeaderCommit > int64(s.CommitIndex) {
		s.CommitIndex = min(uint64(request.LeaderCommit), uint64(len(s.Logs))-1)
	}
	s.mu.Unlock()
	return &resp, nil
}

func (s *Server) SendAppendEntries(ctx context.Context) error {
	// Concurrently send append requets/ healthcheck
	for i := range s.clusterMember {
		go func(i int) {
			next := int64(s.clusterMember[i].NextIndex)

			var entriesRPC []*raft.LogEntry
			if int64(len(s.Logs)-1) >= next {
				entries := s.Logs[next:]
				for _, each := range entries {
					entriesRPC = append(entriesRPC, &raft.LogEntry{Term: int64(each.Term), Index: int64(each.Index), Command: each.Command})
				}
			}

			req := &raft.AppendEntriesRequest{
				Term:         int64(s.CurrentTerm),
				LeaderId:     int64(s.ID),
				PrevLogIndex: next - 1,
				PrevLogterm:  int64(s.Logs[next].Term),
				LeaderCommit: int64(s.CommitIndex),
				Entries:      entriesRPC,
			}

			// Only need to send to established connection
			if s.clusterMember[i].conn != nil {
				resp, err := s.clusterMember[i].service.AppendEntries(ctx, req)
				if err != nil {
					log.Printf("Err: Error requesting response %v", err)
					return
				}
				s.mu.Lock()
				// Check if it is stale response
				if resp.Term != req.Term && s.State.GetCurrentRole() == Leader {
					return
				}

				if resp.Success {
					// Update each folower nextIndex to the index just after the last logs + 1
					// In case there is no log entries yet, the nextIndex of the followers should be 1
					s.clusterMember[i].NextIndex = max(uint64(req.PrevLogIndex)+1+uint64(len(entriesRPC)), 1)
					s.clusterMember[i].MatchIndex = s.clusterMember[i].NextIndex - 1
				} else {
					// If failed, need to decrease the follower's nextIndex
					s.clusterMember[i].NextIndex = max(s.clusterMember[i].NextIndex-1, 1)
				}
			}
		}(i)
	}
	return nil
}
