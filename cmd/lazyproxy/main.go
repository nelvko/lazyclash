package main

import (
	"context"
	"os"
	"syscall"

	"github.com/charmbracelet/fang"
	"github.com/nelvko/lazyclash/internal/cli"
)

func main() {
	if err := fang.Execute(context.Background(), cli.New(), fang.WithVersion("0.1.0-dev"), fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM)); err != nil {
		os.Exit(1)
	}
}
