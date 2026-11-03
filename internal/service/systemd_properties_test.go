package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestCheckReadsNamedManagerStateWithoutValueFlag(t *testing.T) {
	for _, scope := range []Scope{User, System} {
		for _, tc := range []struct {
			name, output string
			wantErr      bool
		}{
			{"running", "SystemState=running\n", false},
			{"degraded", "SystemState=degraded\n", false},
			{"starting", "SystemState=starting\n", false},
			{"other properties", "Version=219\nSystemState=maintenance\n", false},
			{"whitespace", "\nSystemState=running\r\n\n", false},
			{"missing", "", true},
			{"empty", "SystemState=\n", true},
			{"wrong property", "OtherState=running\n", true},
			{"bare value", "running\n", true},
		} {
			t.Run(string(scope)+"/"+tc.name, func(t *testing.T) {
				s, _ := testManager(t)
				s.Scope = scope
				s.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
					if slices.Contains(args, "--value") {
						return nil, errors.New("unrecognized option '--value'")
					}
					want := []string{"show", "--property=SystemState"}
					if scope == User {
						want = append([]string{"--user"}, want...)
					}
					if command != "systemctl" || !reflect.DeepEqual(args, want) {
						t.Fatalf("unexpected manager query: %s %v", command, args)
					}
					return []byte(tc.output), nil
				}
				if err := s.Check(t.Context()); (err != nil) != tc.wantErr {
					t.Fatalf("Check() = %v, wantErr=%v", err, tc.wantErr)
				}
			})
		}
	}
}

func TestCheckManagerFailureDescribesScopeAndPreservesCause(t *testing.T) {
	for _, scope := range []Scope{User, System} {
		for _, cause := range []error{errors.New("manager connection failed"), context.Canceled, context.DeadlineExceeded} {
			t.Run(string(scope)+"/"+cause.Error(), func(t *testing.T) {
				s, _ := testManager(t)
				s.Scope = scope
				s.run = func(context.Context, string, ...string) ([]byte, error) { return nil, cause }
				err := s.Check(t.Context())
				if !errors.Is(err, cause) {
					t.Fatalf("manager failure lost its cause: %v", err)
				}
				for _, language := range []string{i18n.English, i18n.Chinese} {
					text := i18n.New(language).Error(err)
					if scope == User {
						if !strings.Contains(text, "XDG_RUNTIME_DIR") {
							t.Fatalf("missing user session guidance: %s", text)
						}
					} else {
						manager := "systemd system manager"
						if language == i18n.Chinese {
							manager = "systemd 系统管理器"
						}
						if !strings.Contains(text, manager) || strings.Contains(text, "XDG_RUNTIME_DIR") {
							t.Fatalf("incorrect system manager guidance: %s", text)
						}
					}
				}
			})
		}
	}
}

func TestAvailableRequiresExplicitNamedNotFoundState(t *testing.T) {
	queryErr := errors.New("manager query failed")
	for _, scope := range []Scope{User, System} {
		for _, tc := range []struct {
			name, output string
			queryErr     error
			available    bool
		}{
			{"absent", "LoadState=not-found\n", nil, true},
			{"other properties", "ActiveState=inactive\nLoadState=not-found\n", nil, true},
			{"vendor unit", "LoadState=loaded\n", nil, false},
			{"masked unit", "LoadState=masked\n", nil, false},
			{"invalid unit", "LoadState=error\n", nil, false},
			{"missing", "", nil, false},
			{"empty", "LoadState=\n", nil, false},
			{"wrong property", "OtherState=not-found\n", nil, false},
			{"bare value", "not-found\n", nil, false},
			{"failed query", "LoadState=not-found\n", queryErr, false},
		} {
			t.Run(string(scope)+"/"+tc.name, func(t *testing.T) {
				s, _ := testManager(t)
				s.Scope = scope
				s.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
					if slices.Contains(args, "--value") {
						return nil, errors.New("unrecognized option '--value'")
					}
					want := []string{"show", s.Name + ".service", "--property=LoadState"}
					if scope == User {
						want = append([]string{"--user"}, want...)
					}
					if command != "systemctl" || !reflect.DeepEqual(args, want) {
						t.Fatalf("unexpected availability query: %s %v", command, args)
					}
					return []byte(tc.output), tc.queryErr
				}
				if tc.available {
					if err := s.Available(t.Context()); err != nil {
						t.Fatal(err)
					}
					return
				}
				err := s.Install(t.Context(), "/mihomo", "/config", "/data")
				if err == nil || tc.queryErr != nil && !errors.Is(err, tc.queryErr) {
					t.Fatalf("unsafe availability result: %v", err)
				}
				if _, err := os.Lstat(filepath.Join(s.UnitDir, s.Name+".service")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("rejected availability published a unit: %v", err)
				}
			})
		}
	}
}
