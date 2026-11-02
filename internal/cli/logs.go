package cli

import (
	"context"
	"errors"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/spf13/cobra"
)

func newLogsCommand(backend func() *app.App) *cobra.Command {
	lines, duration := 200, 5*time.Second
	cmd := &cobra.Command{Use: "logs", Short: "Observe a bounded window of live core logs as redacted JSON (no history)", Args: noArgs}
	cmd.Flags().IntVar(&lines, "lines", lines, "Maximum new entries to collect (1–500)")
	cmd.Flags().DurationVar(&duration, "duration", duration, "Observation window (greater than zero, at most 1m)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if lines < 1 || lines > 500 || duration <= 0 || duration > time.Minute {
			return diagnostic.New("diagnostic.cli.logs_window", "logs requires --lines 1–500 and --duration greater than zero and at most 1m")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), duration)
		defer cancel()
		entries := make([]mihomo.LogEntry, 0, lines)
		complete := false
		err := backend().StreamLogs(ctx, func(entry mihomo.LogEntry) {
			if len(entries) >= lines {
				return
			}
			entries = append(entries, entry)
			if len(entries) == lines {
				complete = true
				cancel()
			}
		})
		if cmd.Context().Err() != nil {
			return cmd.Context().Err()
		}
		if err != nil && !complete {
			if errors.Is(err, app.ErrLogsNotReady) || !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
		}
		return writeJSON(cmd, entries)
	}
	return cmd
}
