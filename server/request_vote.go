package server

import (
	"context"
	"log"
	"math"

	raft "github.com/jeffreylean/goraft/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

func (s *Server) RequestVote(ctx context.Context, request *raft.RequestVoteRequest) (*raft.RequestVoteResponse, error) {
	if request.Term < int64(s.CurrentTerm) {
		return &raft.RequestVoteResponse{Term: int64(s.CurrentTerm), VoteGranted: false}, nil
	}
	if s.VotedFor != uint64(request.CandidateId) && s.CurrentTerm != uint64(request.Term) {
		s.VotedFor = uint64(request.CandidateId)
		s.CurrentTerm = uint64(request.Term)
		return &raft.RequestVoteResponse{Term: int64(s.CurrentTerm), VoteGranted: true}, nil
	}
	return &raft.RequestVoteResponse{Term: int64(s.CurrentTerm), VoteGranted: false}, nil
}

func (s *Server) SendRequestVote(ctx context.Context, term, serverId, lastLogIndex, lastLogTerm int64) (uint64, bool, error) {
	req := &raft.RequestVoteRequest{
		Term:         term,
		CandidateId:  serverId,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	// Number of vote needed to win an election
	majorityCount := math.Floor((float64(len(s.clusterMember)) / 2) + 1)
	// Count number of current vote
	// Start from one because candidate vote for itself
	voteCount := 1
	// Response channel
	respChan := make(chan *raft.RequestVoteResponse)

	// Concurrently send vote requets
	for _, member := range s.clusterMember {
		go func(s raft.RaftServiceClient, conn *grpc.ClientConn) {
			// Only need to send to established connection
			if conn != nil {
				if conn.GetState() == connectivity.Ready {
					resp, err := s.RequestVote(ctx, req)
					if err != nil {
						log.Printf("Err: Error requesting response %v", err)
					}
					respChan <- resp
				}
			}
		}(member.service, member.conn)
	}

	for {
		select {
		case resp := <-respChan:
			// If response term is higher, means the candidate is out of date, and there's another server become a leader.
			// The candidate will have to update it's term to the higher term.
			if resp.Term > term {
				// TODO update own term to higher term
				log.Println("Candidate out dated, become follower instead you sucker!")
				return uint64(resp.Term), false, nil

			}
			if resp.VoteGranted {
				voteCount++
			}
		default:
			if voteCount >= int(majorityCount) {
				// TODO change the state of the server to become new leader.
				log.Printf("%v has become a new leader for term %d", serverId, term)
				return uint64(term), true, nil
			}
		}
	}
}
