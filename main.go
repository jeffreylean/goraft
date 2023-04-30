package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jeffreylean/goraft/server"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errChan := make(chan error, 1)

	var path = flag.String("p", "config.yaml", "")
	flag.Parse()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	s := server.Create(*path)
	go func() {
		<-sigCh
		cancel()
		s.GrpcServer.GracefulStop()
	}()

	go s.Start(ctx, errChan)

	select {
	case <-ctx.Done():
		fmt.Println("Gracefully shutting down server")
		time.Sleep(1 * time.Second)
	case err := <-errChan:
		fmt.Println("Err:", err.Error())
		fmt.Println("Gracefully shutting down server")
		s.GrpcServer.GracefulStop()
		time.Sleep(1 * time.Second)
	}
}
