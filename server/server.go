package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/jeffreylean/goraft/client"
	"github.com/jeffreylean/goraft/config"
	l "github.com/jeffreylean/goraft/log"
	raft "github.com/jeffreylean/goraft/proto"
	"google.golang.org/grpc"
)

type Server struct {
	GrpcServer *grpc.Server
	Cancel     context.CancelFunc
	// Current Term. Last term seen by the server. It increments from 0 and increases monotonically. The leader will increment by 1 before sending AppendEntries the the follower.
	// The follower will compare this term with the incoming Term (typically from leader) which will be populated in AppendEntries, if incoming term is higher than the
	// CurrentTerm, it updates its CurrentTerm to the higher value and transition to follower state.
	// If a candidate or leader discovers that its term is higher than incoming term, it will reject the request.
	CurrentTerm int
	// MatchIndex is an array that store highest replcated log index of all the server. The length should be the number of servers, and each array's index holds the respective server's highest index of a log entry that has
	// been replicated on that server.
	MatchIndex []int
	// Array that store the next log entry that the leader should send to the server. When leader sends AppendEntries to followers, it will includes the NextIndex for each follower.
	// If a follower's Nextindex is less than the index of the first new entry being set, it means that there are missing entries on that follower's log, and the leader will have
	// to decrements NextIndex for that follower and retries sending AppendEntries until all missing entires have been replicated.
	NextIndex []int
	// The index of highest log entry that has been committed by a majority of servers in the cluster.
	CommitIndex int
	// The index of the highest log entry that has been applied to the server's state machine.
	LastApplied int
	Role        Role
	VotedFor    int
	Logs        []l.LogEntry
	State       *State

	cluster            config.Cluster
	mu                 sync.Mutex
	healthCheckTimeOut time.Time
	electionTimeOut    time.Time
	client             *client.Client
}

func Create(path string) *Server {
	// Load config
	conf := config.Load(path)
	s := Server{
		GrpcServer:         grpc.NewServer(),
		healthCheckTimeOut: time.Now(),
		cluster:            conf.Cluster,
		mu:                 sync.Mutex{},
	}
	raft.RegisterRaftServiceServer(s.GrpcServer, &s)

	return &s
}

func (s *Server) Start(ctx context.Context, errChan chan error) {
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cluster.Port))
	defer listen.Close()
	if err != nil {
		log.Printf("Err:%v", err)
		errChan <- err
	}

	log.Printf("server: Creating connection with %v", s.cluster.Routes)
	c, err := client.Dial(s.cluster.Routes)
	if err != nil {
		log.Println("Sever:", err)
	}
	s.client = c
	s.InitializeState(ctx)

	log.Printf("server: Listening for grpc on :%d", s.cluster.Port)
	if err := s.GrpcServer.Serve(listen); err != nil {
		log.Printf("Server Err:%v", err)
		// Close all connection
		for conn := range c.ServiceConn {
			if conn != nil {
				conn.Close()
			}
		}
		errChan <- err
	}
}

func (s *Server) AppendEntries(ctx context.Context, request *raft.AppendEntriesRequest) (*raft.AppendEntriesResponse, error) {
	// It's a healthcheck
	if len(request.Entries) == 0 {
		log.Printf("Health check received from leader %d", request.LeaderId)
		s.HealthCheckTimer()
	}
	return &raft.AppendEntriesResponse{Success: true, Term: int64(s.CurrentTerm)}, nil
}

func (s *Server) RequestVote(ctx context.Context, request *raft.RequestVoteRequest) (*raft.RequestVoteResponse, error) {
	if request.Term < int64(s.CurrentTerm) {
		return &raft.RequestVoteResponse{Term: int64(s.CurrentTerm), VoteGranted: false}, nil
	}
	if s.VotedFor != int(request.CandidateId) && s.CurrentTerm != int(request.Term) {
		s.VotedFor = int(request.CandidateId)
		s.CurrentTerm = int(request.Term)
		return &raft.RequestVoteResponse{Term: int64(s.CurrentTerm), VoteGranted: true}, nil
	}
	return &raft.RequestVoteResponse{Term: int64(s.CurrentTerm), VoteGranted: false}, nil
}
