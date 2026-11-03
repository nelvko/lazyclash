// Package app implements the use cases shared by the TUI and CLI.
package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

const DefaultService = "lazyclash-mihomo"

type Services interface {
	Check(context.Context) error
	Available(context.Context) error
	Inspect(context.Context) (service.Status, error)
	Install(context.Context, string, string, string) error
	Verify(context.Context, string, string, string) error
	RemoveOwned(context.Context, string, string, string) error
	Action(context.Context, string) error
}

type App struct {
	Home, Name    string
	Service       Services
	PlatformError error
	// Replacements are narrow seams for deterministic business/HTTP tests.
	Download        func(context.Context, string, func(string)) error
	Version         func(context.Context, string) (string, error)
	Validate        func(context.Context, string, string, string) error
	HTTP            *http.Client
	CheckPorts      func(controller string, mixedPort int) error
	ProbeProxy      func(context.Context, int) error
	Resolve         func(context.Context, string, downloadnet.Config) (mihomo.Release, error)
	QueryLatest     func(context.Context, mihomo.Channel, downloadnet.Config) (string, error)
	DownloadRelease func(context.Context, string, mihomo.Release, downloadnet.Config, func(mihomo.Progress)) error
	TestFile        func(context.Context, mihomo.Release, downloadnet.Config) error
	DiskSpace       func(string) (uint64, error)
}

func New(home, name string) (*App, error) {
	if name == "" && !(runtime.GOOS == "linux" && os.Geteuid() == 0) {
		name = os.Getenv("LAZYCLASH_SERVICE")
	}
	if name == "" {
		name = DefaultService
	}
	if !regexp.MustCompile(`^lazyclash-[A-Za-z0-9-]+$`).MatchString(name) {
		return nil, diagnostic.New("diagnostic.app.serviceName", "invalid lazyclash service name")
	}
	root, err := store.HomeForService(home, name)
	if err != nil {
		return nil, err
	}
	svc, platformErr := service.New(name)
	a := &App{Home: root, Name: name, PlatformError: platformErr, Download: mihomo.Download, Version: mihomo.BinaryVersion, Validate: mihomo.Validate}
	if svc != nil {
		if svc.Scope == service.System {
			svc.Home = root
			svc.Lifecycle = filepath.Join(root, "installation", "lazyclash")
		}
		a.Service = svc
	}
	return a, nil
}

func (a *App) locked(ctx context.Context, fn func() error) error {
	if a.systemService() != nil {
		if err := trustedRootAncestors(a.Home); err != nil {
			return err
		}
	}
	unlock, err := store.Lock(ctx, a.Home)
	if err != nil {
		return err
	}
	defer unlock()
	if a.systemService() != nil {
		if err := trustedRootAncestors(a.Home); err != nil {
			return err
		}
	}
	if err := a.recover(ctx); err != nil {
		return err
	}
	return fn()
}
