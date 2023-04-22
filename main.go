package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jeffreylean/goraft/server"
)

type LogEntry struct {
	Index   int
	Term    int
	Command string
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var port = flag.Int("p", 8081, "")
	var id = flag.Int("i", 1, "")
	flag.Parse()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	s := server.Create(*(port), *(id))
	go func() {
		<-sigCh
		cancel()
		s.GrpcServer.GracefulStop()
	}()

	if err := s.Start(ctx); err != nil {
		log.Printf("Err:%v", err)
		os.Exit(1)
	}
}
