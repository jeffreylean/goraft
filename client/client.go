package client

import (
	"fmt"

	raft "github.com/jeffreylean/goraft/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

type Client struct {
	ServiceConn map[*grpc.ClientConn]raft.RaftServiceClient
}

func DialAsync(routes []string, connChan chan *grpc.ClientConn) {
	for _, addr := range routes {
		go func(addr string) {
			for {
				conn, err := grpc.Dial(addr, grpc.WithInsecure())
				if err == nil && conn.GetState() == connectivity.Ready {
					fmt.Println("connected")
					connChan <- conn
					return
				}
			}
		}(addr)
	}
}

func Dial(routes []string) (*Client, error) {
	c := Client{ServiceConn: make(map[*grpc.ClientConn]raft.RaftServiceClient)}
	for _, addr := range routes {
		conn, err := grpc.Dial(addr, grpc.WithInsecure())
		if err == nil {
			service := raft.NewRaftServiceClient(conn)
			c.ServiceConn[conn] = service
		}
	}
	return &c, nil
}
