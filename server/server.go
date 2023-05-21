package server

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/jeffreylean/goraft/config"
	l "github.com/jeffreylean/goraft/log"
	raft "github.com/jeffreylean/goraft/proto"
	"google.golang.org/grpc"
)

type Server struct {
	config.Cluster
	GrpcServer *grpc.Server
	Cancel     context.CancelFunc
	// Current Term. Last term seen by the server. It increments from 0 and increases monotonically. The leader will increment by 1 before sending AppendEntries the the follower.
	// The follower will compare this term with the incoming Term (typically from leader) which will be populated in AppendEntries, if incoming term is higher than the
	// CurrentTerm, it updates its CurrentTerm to the higher value and transition to follower state.
	// If a candidate or leader discovers that its term is higher than incoming term, it will reject the request.
	CurrentTerm uint64
	// The index of highest log entry that has been committed by a majority of servers in the cluster.
	CommitIndex uint64
	// The index of the highest log entry that has been applied to the server's state machine.
	LastApplied        uint64
	Role               Role
	VotedFor           uint64
	Logs               []l.LogEntry
	State              *State
	clusterMember      []Member
	mu                 sync.Mutex
	healthCheckTimeOut time.Time
	electionTimeOut    time.Time
}

type Member struct {
	Addr string
	// Array that store the next log entry that the leader should send to the server. When leader sends AppendEntries to followers, it will includes the NextIndex for each follower.
	// If a follower's Nextindex is less than the index of the first new entry being set, it means that there are missing entries on that follower's log, and the leader will have
	// to decrements NextIndex for that follower and retries sending AppendEntries until all missing entires have been replicated.
	NextIndex uint64
	// MatchIndex is an array that store highest replcated log index of all the server. The length should be the number of servers, and each array's index holds the respective server's highest index of a log entry that has
	// been replicated on that server.
	MatchIndex uint64
	conn       *grpc.ClientConn
	service    raft.RaftServiceClient
}

func Create(path string) *Server {
	// Load config
	conf := config.Load(path)
	clusterMember := make([]Member, len(conf.Cluster.Routes))
	for i, each := range conf.Cluster.Routes {
		clusterMember[i] = Member{
			Addr: each,
		}
	}
	s := Server{
		GrpcServer:         grpc.NewServer(),
		healthCheckTimeOut: time.Now(),
		electionTimeOut:    time.Now(),
		clusterMember:      clusterMember,
		mu:                 sync.Mutex{},
		Logs:               make([]l.LogEntry, 0),
	}
	s.ID = conf.Cluster.ID
	s.Port = conf.Cluster.Port
	s.Host = conf.Cluster.Host
	s.Routes = conf.Cluster.Routes

	raft.RegisterRaftServiceServer(s.GrpcServer, &s)

	return &s
}

func (s *Server) Start(ctx context.Context, errChan chan error) {
	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", s.Port))
	defer listen.Close()
	if err != nil {
		log.Printf("Err:%v", err)
		errChan <- err
	}

	log.Printf("server: Creating connection with %v", s.Routes)
	for _, member := range s.clusterMember {
		conn, err := grpc.Dial(member.Addr, grpc.WithInsecure())
		if err == nil {
			service := raft.NewRaftServiceClient(conn)
			member.conn = conn
			member.service = service
		}
	}

	s.InitializeState(ctx)

	log.Printf("server: Listening for grpc on :%d", s.Port)
	if err := s.GrpcServer.Serve(listen); err != nil {
		log.Printf("Server Err:%v", err)
		// Close all connection
		for _, member := range s.clusterMember {
			if member.conn != nil {
				member.conn.Close()
			}
		}
		errChan <- err
	}
}

func (s *Server) HealthCheckTimer() {
	healthCheckInterval := time.Duration(time.Millisecond * 50)
	s.mu.Lock()
	s.healthCheckTimeOut = time.Now().Add(healthCheckInterval)
	s.mu.Unlock()
}

func (s *Server) ElectionTimer() {
	duration := rand.Intn(300-150) + 150
	s.mu.Lock()
	s.electionTimeOut = time.Now().Add(time.Millisecond * time.Duration(duration))
	s.mu.Unlock()
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

func parseLog(log []*raft.LogEntry) []l.LogEntry {
	output := make([]l.LogEntry, len(log))
	for i := 0; i < len(log); i++ {
		output[i] = l.LogEntry{
			Index:   int(log[i].Index),
			Term:    int(log[i].Term),
			Command: log[i].Command,
		}
	}
	return output
}

func min[t ~int | ~int64 | ~uint64](a, b t) t {
	if a < b {
		return a
	}
	return b
}

func max[t ~int | ~int64 | ~uint64](a, b t) t {
	if a > b {
		return a
	}
	return b
}
