package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/droxey/x3vault/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Run(ctx, os.Args)
}
