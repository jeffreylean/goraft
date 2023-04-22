package server

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"

	raft "github.com/jeffreylean/goraft/proto"
	"google.golang.org/grpc"
)

type Role int

const (
	Leader    Role = 0
	Follower  Role = 1
	Candidate Role = 1
)

type Server struct {
	GrpcServer *grpc.Server
	port       int
	Cancel     context.CancelFunc
	// Server ID
	ID int
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

	timerChan       chan bool
	healthCheckChan chan bool
}

func Create(port int, id int) *Server {
	s := Server{
		ID:              id,
		GrpcServer:      grpc.NewServer(),
		port:            port,
		timerChan:       make(chan bool),
		healthCheckChan: make(chan bool),
	}
	raft.RegisterRaftServiceServer(s.GrpcServer, &s)

	return &s
}

func (s *Server) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.Cancel = cancel

	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	defer listen.Close()
	if err != nil {
		return err
	}

	go func(ctx context.Context) {
		ctx, cancel = context.WithCancel(ctx)
		log.Printf("server: Listening for grpc on :%d", s.port)
		if err := s.GrpcServer.Serve(listen); err == nil {
			log.Printf("Err:%v", err)
			cancel()
		}
	}(ctx)

	s.StartTimer()
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
		case <-s.timerChan:
			fmt.Println("New election!!")
		case <-s.healthCheckChan:
			fmt.Println("reset timer!!")
			s.StartTimer()
		}
	}
}

func (s *Server) StartTimer() {
	go func() {
		duration := rand.Intn(300-150) + 150
		time.Sleep(time.Microsecond * time.Duration(duration))
		s.healthCheckChan <- true
	}()
	go func() {
		duration := rand.Intn(300-150) + 150
		time.Sleep(time.Microsecond * time.Duration(duration))
		s.timerChan <- true
	}()
}

func (s *Server) AppendEntries(ctx context.Context, request *raft.AppendEntriesRequest) (*raft.AppendEntriesResponse, error) {
	return nil, nil
}

func (s *Server) RequestVote(ctx context.Context, request *raft.RequestVoteRequest) (*raft.RequestVoteResponse, error) {
	return nil, nil
}
