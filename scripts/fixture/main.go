// Command fixture drives shared app operations in end-to-end tests. It is
// built beside the test's product binary and never included in release packages.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/spf13/pflag"
)

func main() {
	home := flag.String("home", "", "Dedicated test data directory")
	name := flag.String("service-name", "", "Dedicated test service")
	flag.Parse()
	if !filepath.IsAbs(*home) || !strings.HasPrefix(*name, "lazyclash-") || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "fixture requires an explicit test home, service name and operation")
		os.Exit(2)
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		// The system scope harness preflights and owns these instances.
		if (*name != app.DefaultService && !strings.HasPrefix(*name, "lazyclash-scopes-")) || *home != filepath.Join("/var/lib/lazyclash", *name) {
			fmt.Fprintln(os.Stderr, "expected the system scope harness's dedicated instance")
			os.Exit(2)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a, err := app.New(*home, *name)
	if err == nil {
		err = run(ctx, a, flag.Args())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func output[T any](v T, err error) error {
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(v)
}

func run(ctx context.Context, a *app.App, args []string) error {
	switch args[0] {
	case "nodes":
		s, err := a.Snapshot(ctx)
		if err != nil {
			return err
		}
		if !s.Installed || s.APIError != "" || s.ServiceError != "" {
			return errors.New("fixture needs running core and reachable API")
		}
		return output(s.Groups, nil)
	case "select":
		if len(args) == 3 {
			return a.Select(ctx, args[1], args[2])
		}
	case "rules":
		s, err := a.Rules(ctx)
		if err != nil {
			return err
		}
		if s.State != "ready" {
			return fmt.Errorf("rules unavailable: %s", s.State)
		}
		return output(s.Rules, nil)
	case "stats":
		return output(a.Runtime(ctx))
	case "connections":
		if len(args) == 1 {
			return output(a.Connections(ctx))
		}
		if len(args) == 3 && args[1] == "close" {
			return a.CloseConnection(ctx, args[2])
		}
	case "mode":
		if len(args) == 2 {
			return a.SetMode(ctx, args[1])
		}
	case "autostart":
		if len(args) == 2 && (args[1] == "enable" || args[1] == "disable") {
			return a.SetAutostart(ctx, args[1] == "enable")
		}
	case "tun":
		if len(args) == 2 && args[1] == "off" {
			return a.SetTUN(ctx, false)
		}
		if len(args) == 5 && args[1] == "on" && args[2] == "--scope" && args[3] == "host" && args[4] == "--confirm-host" {
			return a.SetTUN(ctx, true)
		}
	case "profile":
		if len(args) > 1 {
			return profile(ctx, a, args[1:])
		}
	case "settings":
		if len(args) == 2 && args[1] == "show" {
			return output(a.GetRuntimeSettings(ctx))
		}
		if len(args) > 2 && args[1] == "ports" {
			return ports(ctx, a, args[2:])
		}
	case "logs":
		return logs(ctx, a, args[1:])
	}
	return errors.New("unrecognized fixture operation or arguments")
}

func profile(ctx context.Context, a *app.App, args []string) error {
	switch args[0] {
	case "list":
		return output(a.ListProfiles(ctx))
	case "deactivate":
		return a.DeactivateProfile(ctx, "")
	case "add":
		flags := pflag.NewFlagSet("add", pflag.ContinueOnError)
		name := flags.String("name", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() == 1 {
			return a.AddSubscription(ctx, *name, flags.Arg(0))
		}
	case "show":
		if len(args) == 2 {
			return output(a.ProfileDetails(ctx, args[1]))
		}
	case "apply":
		if len(args) == 2 {
			return a.Apply(ctx, args[1])
		}
	case "refresh":
		if len(args) == 2 {
			return a.Refresh(ctx, args[1])
		}
	case "override":
		if len(args) == 3 {
			raw, err := os.ReadFile(args[2])
			if err != nil {
				return err
			}
			return a.SetProfileOverrides(ctx, args[1], raw)
		}
	case "delete":
		if len(args) >= 2 {
			target, err := a.ProfileDetails(ctx, args[1])
			if err != nil {
				return err
			}
			switch {
			case len(args) == 2:
				return a.DeleteProfileWithReplacement(ctx, target.ID, "")
			case len(args) == 3 && args[2] == "--no-profile":
				return a.DeleteProfileWithReplacement(ctx, target.ID, "default")
			case len(args) == 4 && args[2] == "--use":
				other, err := a.ProfileDetails(ctx, args[3])
				if err != nil {
					return err
				}
				return a.DeleteProfileWithReplacement(ctx, target.ID, other.ID)
			}
		}
	}
	return errors.New("unrecognized profile fixture arguments")
}

func ports(ctx context.Context, a *app.App, args []string) error {
	old, err := a.GetRuntimeSettings(ctx)
	if err != nil {
		return err
	}
	proposed := old
	flags := pflag.NewFlagSet("ports", pflag.ContinueOnError)
	flags.IntVar(&proposed.MixedPort, "mixed-port", old.MixedPort, "")
	flags.IntVar(&proposed.ControllerPort, "controller-port", old.ControllerPort, "")
	preview := flags.Bool("dry-run", false, "")
	restart := flags.Bool("restart", false, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	plan, err := a.PrepareRuntimeSettingsEdit(ctx, old, proposed)
	if err != nil {
		return err
	}
	if *preview {
		return output(plan, nil)
	}
	if plan.Active && plan.Changed && !*restart {
		return errors.New("changing running ports requires restart acknowledgement")
	}
	return output(a.ApplyRuntimeSettings(ctx, plan))
}

func logs(ctx context.Context, a *app.App, args []string) error {
	flags := pflag.NewFlagSet("logs", pflag.ContinueOnError)
	lines := flags.Int("lines", 3, "")
	duration := flags.Duration("duration", 4*time.Second, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	stream, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()
	var entries []mihomo.LogEntry
	err := a.StreamLogs(stream, func(entry mihomo.LogEntry) {
		if len(entries) < *lines {
			entries = append(entries, entry)
		}
		if len(entries) == *lines {
			cancel()
		}
	})
	if err != nil && len(entries) != *lines && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return output(entries, ctx.Err())
}
