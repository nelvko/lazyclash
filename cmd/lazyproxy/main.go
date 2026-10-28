package main

import (
	"context"
	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
	"os"
)

func main() {
	root := &cobra.Command{Use: "lazyclash", Short: "A terminal UI for managing Mihomo"}
	if err := fang.Execute(context.Background(), root, fang.WithVersion("0.1.0-dev")); err != nil {
		os.Exit(1)
	}
}
