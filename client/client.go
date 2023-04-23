package client

import (
	"context"
	"fmt"
	"log"
	"math"

	raft "github.com/jeffreylean/goraft/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

type Client struct {
	ServiceConn map[*grpc.ClientConn]raft.RaftServiceClient
}

func Dial(routes []string) (*Client, error) {
	client := &Client{
		ServiceConn: make(map[*grpc.ClientConn]raft.RaftServiceClient, 0),
	}

	for _, addr := range routes {
		conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			return nil, err
		}
		service := raft.NewRaftServiceClient(conn)
		client.ServiceConn[conn] = service
	}
	return client, nil
}

func (c *Client) RequestVote(ctx context.Context, term, serverId, lastLogIndex, lastLogTerm int64) (int64, bool, error) {
	req := &raft.RequestVoteRequest{
		Term:         term,
		CandidateId:  serverId,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	// Number of vote needed to win an election
	majorityCount := math.Floor((float64(len(c.ServiceConn)) / 2) + 1)
	// Count number of current vote
	// Start from one because candidate vote for itself
	voteCount := 1
	// Response channel
	respChan := make(chan *raft.RequestVoteResponse)

	// Concurrently send vote requets
	for conn, service := range c.ServiceConn {
		go func(s raft.RaftServiceClient) {
			// Only need to send to established connection
			fmt.Println(conn.GetState())
			if conn.GetState() == connectivity.Ready {
				resp, err := s.RequestVote(ctx, req)
				if err != nil {
					log.Printf("Err: Error requesting response %v", err)
				}
				respChan <- resp
			}
		}(service)
	}

	for {
		select {
		case resp := <-respChan:
			// If response term is higher, means the candidate is out of date, and there's another server become a leader.
			// The candidate will have to update it's term to the higher term.
			if resp.Term > term {
				// TODO update own term to higher term
				log.Println("Candidate out dated, become follower instead you sucker!")
				return resp.Term, false, nil

			}
			if resp.VoteGranted {
				voteCount++
			}
		default:
			if voteCount >= int(majorityCount) {
				// TODO change the state of the server to become new leader.
				log.Printf("%v has become a new leader for term %d", serverId, term)
				return term, true, nil
			}
		}
	}
}

func (c *Client) AppendEntries(ctx context.Context, term, leaderId, prevLogIndex, leaderCommit int64, logEntries []*raft.LogEntry) error {

	req := &raft.AppendEntriesRequest{
		Term:         term,
		LeaderId:     leaderId,
		PrevLogIndex: prevLogIndex,
		LeaderCommit: leaderCommit,
		Entries:      logEntries,
	}

	// Concurrently send append requets/ healthcheck
	for conn, service := range c.ServiceConn {
		go func(s raft.RaftServiceClient) {
			// Only need to send to established connection
			fmt.Println(conn.GetState())
			if conn.GetState() == connectivity.Ready {
				_, err := s.AppendEntries(ctx, req)
				if err != nil {
					log.Printf("Err: Error requesting response %v", err)
				}
			}
		}(service)
	}
	return nil
}
