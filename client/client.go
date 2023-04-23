package client

import (
	"context"
	"log"
	"math"

	raft "github.com/jeffreylean/goraft/proto"
	"google.golang.org/grpc"
)

type Client struct {
	RaftService []raft.RaftServiceClient
	Conn        []*grpc.ClientConn
}

func Dial(routes []string) (*Client, error) {
	client := &Client{
		RaftService: make([]raft.RaftServiceClient, 0),
		Conn:        make([]*grpc.ClientConn, 0),
	}

	for _, addr := range routes {
		conn, err := grpc.Dial(addr)
		if err != nil {
			return nil, err
		}
		client.Conn = append(client.Conn, conn)
		service := raft.NewRaftServiceClient(conn)
		client.RaftService = append(client.RaftService, service)
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
	majorityCount := math.Floor((float64(len(c.Conn)) / 2) + 1)
	// Count number of current vote
	// Start from one because candidate vote for itself
	voteCount := 1
	// Response channel
	respChan := make(chan *raft.RequestVoteResponse)

	// Concurrently send vote requets
	for _, each := range c.RaftService {
		go func(s raft.RaftServiceClient) {
			resp, err := s.RequestVote(ctx, req)
			if err != nil {
				log.Printf("Err: Error requesting response %v", err)
			}
			respChan <- resp
		}(each)
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
	for _, each := range c.RaftService {
		go func(s raft.RaftServiceClient) {
			_, err := s.AppendEntries(ctx, req)
			if err != nil {
				log.Printf("Err: Error requesting response %v", err)
			}
		}(each)
	}
	return nil
}
