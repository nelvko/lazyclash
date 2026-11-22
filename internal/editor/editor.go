// Package editor resolves a user's terminal editor and passes the document as
// one literal argument. The editor runs on the host where lazyclash runs.
package editor

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/google/shlex"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func arguments(setting string) ([]string, error) {
	// An executable path containing spaces can also be entered directly.
	if path, err := exec.LookPath(setting); err == nil {
		return []string{path}, nil
	}
	args, err := shlex.Split(setting)
	if err != nil || len(args) == 0 {
		return nil, diagnostic.New("editor.invalid", "Enter an executable name or path with optional arguments.")
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return nil, diagnostic.New("editor.unavailable", "Editor executable was not found on this host.")
	}
	args[0] = path
	return args, nil
}

func Validate(setting string) error {
	if setting == "" {
		return nil
	}
	_, err := arguments(setting)
	return err
}

// Command does not run a shell, expand variables or interpret operators.
func Command(ctx context.Context, preference, path string) (*exec.Cmd, error) {
	if preference != "" {
		args, err := arguments(preference)
		if err != nil {
			return nil, err
		}
		return exec.CommandContext(ctx, args[0], append(args[1:], path)...), nil
	}
	for _, candidate := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR"), "nano", "vi"} {
		if option := strings.TrimSpace(candidate); option != "" {
			if args, err := arguments(option); err == nil {
				return exec.CommandContext(ctx, args[0], append(args[1:], path)...), nil
			}
		}
	}
	return nil, diagnostic.New("editor.none", "No editor found. Choose an external editor in Settings.")
}
