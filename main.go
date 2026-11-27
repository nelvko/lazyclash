package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nelvko/lazyclash/internal/cli"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	done := make(chan error, 1)
	go func() { done <- cli.Execute(ctx, args) }()
	select {
	case err := <-done:
		return cli.ExitCode(err)
	case sig := <-signals:
		cancel()
		<-done // Allow commands to finish rollback and restore the terminal.
		return 128 + int(sig.(syscall.Signal))
	}
}
