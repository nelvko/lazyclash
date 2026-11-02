package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nelvko/lazyclash/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := cli.Execute(ctx, os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
