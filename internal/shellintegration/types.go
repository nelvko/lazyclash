// Package shellintegration coordinates a calling shell's proxy request with its
// in-process prompt hook. It never changes the environment of a parent process.
package shellintegration

import "time"

const (
	SessionVariable = "LAZYCLASH_SHELL_SESSION"
	LoadedVariable  = "LAZYCLASH_SHELL_LOADED"
)

type Manager struct {
	HomeDir      string
	ConfigDir    string
	StateDir     string
	caller       func() (Identity, error)
	attachCaller func(string, int) (Identity, error)
	alive        func(Identity) bool
}

type Endpoint struct {
	Namespace string `json:"namespace"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
}

type Status struct {
	Shell             string `json:"shell"`
	SessionID         string `json:"session_id,omitempty"`
	State             string `json:"state"`
	Installed         bool   `json:"installed"`
	Loaded            bool   `json:"loaded"`
	HookConfirmed     bool   `json:"hook_confirmed"`
	Desired           bool   `json:"desired"`
	Applied           bool   `json:"applied"`
	Pending           bool   `json:"pending"`
	ActivationCommand string `json:"activation_command,omitempty"`
	Detail            string `json:"detail,omitempty"`
	Diagnostic        error  `json:"-"`
	Revision          uint64 `json:"revision"`
	MixedPort         int    `json:"mixed_port,omitempty"`
}

type Attachment struct {
	SessionID    string
	SessionDir   string
	EndpointFile string
}

type Identity struct {
	UID          int    `json:"uid"`
	PID          int    `json:"pid"`
	Start        string `json:"start"`
	BootID       string `json:"boot_id"`
	TTY          uint64 `json:"tty"`
	Shell        string `json:"shell"`
	PIDNamespace string `json:"pid_namespace"`
}

type ownerRecord struct {
	SessionID string    `json:"session_id"`
	Owner     Identity  `json:"owner"`
	Namespace string    `json:"namespace,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type pendingRecord struct {
	Owner           Identity  `json:"owner"`
	Enabled         bool      `json:"enabled"`
	Endpoint        Endpoint  `json:"endpoint"`
	Revision        uint64    `json:"revision"`
	UpdatedAt       time.Time `json:"updated_at"`
	NoticeAttempted bool      `json:"notice_attempted"`
}

type Request struct {
	Revision uint64
	Enabled  bool
	Host     string
	Port     int
	Detach   bool
}

type Acknowledgment struct {
	Revision uint64
	Enabled  bool
	Port     int
	Detached bool
	Error    int
}
