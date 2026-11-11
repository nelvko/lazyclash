//go:build linux

package shellintegration

import "testing"

func TestInteractiveInvocationRejectsNoninteractiveCommandAndScripts(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"bash"}, true}, {[]string{"-bash", "--login"}, true}, {[]string{"bash", "--norc", "-i"}, true}, {[]string{"bash", "-ic", "echo test"}, true},
		{[]string{"bash", "-c", "lazyclash"}, false}, {[]string{"bash", "script.sh"}, false}, {[]string{"zsh", "-c", "lazyclash"}, false}, {[]string{"fish", "--command", "lazyclash"}, false},
		{[]string{"bash", "-c", "lazyclash", "-i"}, false}, {[]string{"bash", "script.sh", "-i"}, false}, {[]string{"bash", "--"}, true}, {[]string{"bash", "--", "script.sh"}, false}, {[]string{"bash", "+i"}, false},
		{[]string{"bash", "--rcfile", "/tmp/rc"}, true}, {nil, false},
	} {
		if got := interactiveInvocation(test.args); got != test.want {
			t.Errorf("%q = %v want %v", test.args, got, test.want)
		}
	}
}
