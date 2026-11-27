package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type lifecycleFixture struct {
	action  string
	removed int
	err     error
}

func (f *lifecycleFixture) Control(_ context.Context, action string) error {
	f.action = action
	return f.err
}
func (f *lifecycleFixture) RemoveCore(context.Context) error { f.removed++; return f.err }

func TestUninstallUsesSharedRemovalWithoutStopping(t *testing.T) {
	f := &lifecycleFixture{err: errors.New("stop the core first")}
	cmd := uninstallCommand(func() removalBackend { return f })
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); !errors.Is(err, f.err) || f.removed != 1 || f.action != "" {
		t.Fatal("uninstall hid preconditions or changed lifecycle automatically", err)
	}
}

func TestServiceCommandsUseSharedLifecycle(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			f := &lifecycleFixture{}
			cmd := serviceCommand(action, func() serviceBackend { return f })
			if err := cmd.Execute(); err != nil || f.action != action {
				t.Fatalf("action=%q err=%v", f.action, err)
			}
		})
	}
}
