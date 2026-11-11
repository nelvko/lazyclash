//go:build !linux

package shellintegration

import (
	"fmt"
	"os"
)

func callerIdentity() (Identity, error) {
	return Identity{}, fmt.Errorf("terminal proxy integration requires Linux with /proc and an interactive Bash, Zsh or Fish")
}
func attachIdentity(string, int) (Identity, error) { return callerIdentity() }
func ownerAlive(Identity) bool                     { return false }
func owned(os.FileInfo) bool                       { return true }
