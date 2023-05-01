package server

import (
	"context"
	"fmt"
	"log"
	"math/rand"
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
	config.Cluster
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
	LastApplied        int
	Role               Role
	VotedFor           int
	Logs               []l.LogEntry
	State              *State
	clusterMember      []Member
	mu                 sync.Mutex
	healthCheckTimeOut time.Time
	electionTimeOut    time.Time
	client             *client.Client
}

type Member struct {
	Addr      string
	NextIndex int
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
	c, err := client.Dial(s.Routes)
	if err != nil {
		log.Println("Sever:", err)
	}
	s.client = c
	s.InitializeState(ctx)

	log.Printf("server: Listening for grpc on :%d", s.Port)
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
			s.CurrentTerm = int(request.Term)
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
		s.CommitIndex = min(int(request.LeaderCommit), len(s.Logs)-1)
	}
	s.mu.Unlock()
	return &resp, nil
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

func min[T ~int | ~int64](a, b T) T {
	if a < b {
		return a
	}
	return b
}
