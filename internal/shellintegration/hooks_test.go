package shellintegration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func hookQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// These tests exercise the actual installed script text with an attach fixture.
// Real calling-shell identity and pending migration are covered separately.
func hookFixture(t *testing.T, shell string) (script, dir string) {
	t.Helper()
	dir = t.TempDir()
	session := filepath.Join(dir, "session")
	if err := os.Mkdir(session, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"request": "1 1 1 127.0.0.1 17891 0\n", "endpoint": "1 1 127.0.0.1 17891\n"} {
		if err := os.WriteFile(filepath.Join(session, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(dir, "attach")
	body := "#!/bin/sh\nif [ \"$2\" = _attach ]; then printf '%s\\n' fixture " + hookQuote(session) + " " + hookQuote(filepath.Join(session, "endpoint")) + "; fi\n"
	if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	filename := shell + ".sh"
	if shell == "fish" {
		filename = "fish.fish"
	}
	data, err := integrationScripts.ReadFile(filepath.ToSlash(filepath.Join("scripts", filename)))
	if err != nil {
		t.Fatal(err)
	}
	script = filepath.Join(dir, filename)
	if err := os.WriteFile(script, []byte(strings.ReplaceAll(string(data), "@LAZYCLASH_EXECUTABLE@", hookQuote(binary))), 0600); err != nil {
		t.Fatal(err)
	}
	return script, session
}
func runHookShell(t *testing.T, shell, body string) {
	t.Helper()
	path, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s unavailable", shell)
	}
	cmd := exec.Command(path, "-i", "-c", body)
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "TERM=xterm", "XDG_CONFIG_HOME=" + t.TempDir()}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s failed: %v\n%s\nbody:\n%s", shell, err, output, body)
	}
}
func TestHooksFirstSourceRestoresExactEnvironment(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			script, dir := hookFixture(t, shell)
			if shell == "fish" {
				runHookShell(t, shell, fmt.Sprintf(`
set -gu http_proxy 'original
quote"value'
set -gx HTTP_PROXY 'UPPER original'
set -gx https_proxy ''
set -eg all_proxy; set -eg ALL_PROXY
set -gx no_proxy 'old, localhost'
set -gx NO_PROXY 'UPPER,old'
source %s; or exit 10
test "$http_proxy" = http://127.0.0.1:17891; or exit 11
test "$ALL_PROXY" = socks5h://127.0.0.1:17891; or exit 12
test "$no_proxy" = 'old,localhost,UPPER,127.0.0.1,::1'; or exit 13
source %s; or exit 14
printf '1 2 127.0.0.1 17892\n' > %s/endpoint
__lazyclash_sync
test "$http_proxy" = http://127.0.0.1:17892; or exit 15
printf '1 2 0 127.0.0.1 17892 0\n' > %s/request
__lazyclash_sync
test "$http_proxy" = 'original
quote"value'; or exit 16
set -qgx http_proxy; and exit 17
test "$HTTP_PROXY" = 'UPPER original'; or exit 18
set -qgx HTTP_PROXY; or exit 19
set -qgx https_proxy; and test "$https_proxy" = ''; or exit 20
set -qg all_proxy; and exit 21
set -qg ALL_PROXY; and exit 22
test "$no_proxy" = 'old, localhost'; or exit 23
test "$NO_PROXY" = 'UPPER,old'; or exit 24
exit 0
`, hookQuote(script), hookQuote(script), hookQuote(dir), hookQuote(dir)))
			} else {
				runHookShell(t, shell, fmt.Sprintf(`
http_proxy='original
quote"value'; export -n http_proxy 2>/dev/null || typeset +x http_proxy
export HTTP_PROXY='UPPER original' https_proxy=''
unset all_proxy ALL_PROXY
export no_proxy='old, localhost' NO_PROXY='UPPER,old'
. %s || exit 10
[ "$http_proxy" = http://127.0.0.1:17891 ] || exit 11
[ "$ALL_PROXY" = socks5h://127.0.0.1:17891 ] || exit 12
[ "$no_proxy" = 'old,localhost,UPPER,127.0.0.1,::1' ] || exit 13
. %s || exit 14
printf '1 2 127.0.0.1 17892\n' > %s/endpoint
__lazyclash_sync
[ "$http_proxy" = http://127.0.0.1:17892 ] || exit 15
printf '1 2 0 127.0.0.1 17892 0\n' > %s/request
__lazyclash_sync
[ "$http_proxy" = 'original
quote"value' ] || exit 16
[ "$HTTP_PROXY" = 'UPPER original' ] || exit 18
[ "${https_proxy+x}" = x ] && [ "$https_proxy" = '' ] || exit 20
[ "${all_proxy+x}" != x ] || exit 21
[ "${ALL_PROXY+x}" != x ] || exit 22
[ "$no_proxy" = 'old, localhost' ] || exit 23
[ "$NO_PROXY" = 'UPPER,old' ] || exit 24
python3 -c 'import os; assert "http_proxy" not in os.environ; assert os.environ["HTTP_PROXY"] == "UPPER original"; assert os.environ["https_proxy"] == ""'
exit $?
`, hookQuote(script), hookQuote(script), hookQuote(dir), hookQuote(dir)))
			}
		})
	}
}
func TestBashHookKeepsExistingPromptAndReadonlyVariables(t *testing.T) {
	script, dir := hookFixture(t, "bash")
	runHookShell(t, "bash", fmt.Sprintf(`
PROMPT_COMMAND=': original_prompt'
http_proxy=original
readonly HTTP_PROXY=readonly_value
. %s
[ "$http_proxy" = original ] || exit 1
[ "$HTTP_PROXY" = readonly_value ] || exit 2
case "$PROMPT_COMMAND" in *': original_prompt'*) ;; *) exit 3 ;; esac
. %s
[ "$(printf '%%s' "$PROMPT_COMMAND" | python3 -c 'import sys; print(sys.stdin.read().count("__lazyclash_prompt"))')" = 1 ] || exit 4
read v rev enabled port detached error < %s/ack
[ "$error" = 1 ] || exit 5
false
__lazyclash_prompt "$?" && :
[ "$?" = 1 ] || exit 6
exit 0
`, hookQuote(script), hookQuote(script), hookQuote(dir)))
}
func TestHooksMissingOrCorruptRequestRetainsAppliedEnvironment(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			script, dir := hookFixture(t, shell)
			if shell == "fish" {
				runHookShell(t, shell, fmt.Sprintf(`source %s; or exit 1
printf 'broken\n' > %s/request
__lazyclash_sync
test "$http_proxy" = http://127.0.0.1:17891; or exit 2
rm %s/request
__lazyclash_sync
test "$http_proxy" = http://127.0.0.1:17891; or exit 3
exit 0`, hookQuote(script), hookQuote(dir), hookQuote(dir)))
			} else {
				runHookShell(t, shell, fmt.Sprintf(`. %s || exit 1
printf 'broken\n' > %s/request
__lazyclash_sync
[ "$http_proxy" = http://127.0.0.1:17891 ] || exit 2
rm %s/request
__lazyclash_sync
[ "$http_proxy" = http://127.0.0.1:17891 ] || exit 3
exit 0`, hookQuote(script), hookQuote(dir), hookQuote(dir)))
			}
		})
	}
}

func TestBashPromptArrayAndStatusPreserved(t *testing.T) {
	script, _ := hookFixture(t, "bash")
	runHookShell(t, "bash", fmt.Sprintf(`
if [ "${BASH_VERSINFO[0]}" -lt 5 ] || { [ "${BASH_VERSINFO[0]}" = 5 ] && [ "${BASH_VERSINFO[1]}" -lt 1 ]; }; then exit 0; fi
PROMPT_COMMAND=('existing_one' 'existing_two')
. %s || exit 1
. %s || exit 2
[ "${#PROMPT_COMMAND[@]}" = 3 ] || exit 3
[ "${PROMPT_COMMAND[0]}" = existing_one ] || exit 4
[ "${PROMPT_COMMAND[1]}" = existing_two ] || exit 5
false
__lazyclash_prompt "$?" && :
[ "$?" = 1 ] || exit 6
set -e
false || __lazyclash_prompt "$?" && :
exit 0
`, hookQuote(script), hookQuote(script)))
}

func TestZshPreservesExistingHooksAndDoesNotDuplicate(t *testing.T) {
	script, _ := hookFixture(t, "zsh")
	runHookShell(t, "zsh", fmt.Sprintf(`
existing_precmd() { return 0; }
precmd_functions=(existing_precmd)
. %s || exit 1
. %s || exit 2
[ "${#precmd_functions[@]}" = 2 ] || exit 3
[ "${precmd_functions[1]}" = existing_precmd ] || exit 4
[ "${precmd_functions[2]}" = __lazyclash_precmd ] || exit 5
false
__lazyclash_precmd
[ "$?" = 0 ] || exit 6
exit 0
`, hookQuote(script), hookQuote(script)))
}

func TestFishPreservesUniversalVariablesAndPrompt(t *testing.T) {
	script, dir := hookFixture(t, "fish")
	runHookShell(t, "fish", fmt.Sprintf(`
function fish_prompt; printf 'original prompt'; end
set -Ux HTTP_PROXY 'universal original'
set -Ux no_proxy 'universal.example'
source %s; or exit 1
test "$HTTP_PROXY" = http://127.0.0.1:17891; or exit 2
set -qU HTTP_PROXY; or exit 3
source %s; or exit 4
test (functions --handlers | string match -r '__lazyclash_prompt' | count) = 1; or exit 5
printf '1 2 0 127.0.0.1 17891 0\n' > %s/request
__lazyclash_sync
test "$HTTP_PROXY" = 'universal original'; or exit 6
not set -qg HTTP_PROXY; or exit 7
set -qUx HTTP_PROXY; or exit 8
test "$no_proxy" = universal.example; or exit 9
test (fish_prompt) = 'original prompt'; or exit 10
exit 0
`, hookQuote(script), hookQuote(script), hookQuote(dir)))
}

func TestHooksMissingEndpointRetainsLatestPortButOffStillRestores(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			script, dir := hookFixture(t, shell)
			if shell == "fish" {
				runHookShell(t, shell, fmt.Sprintf(`
source %s; or exit 1
printf '1 2 127.0.0.1 17892\n' > %s/endpoint
__lazyclash_sync
test "$http_proxy" = http://127.0.0.1:17892; or exit 2
rm %s/endpoint
__lazyclash_sync
test "$http_proxy" = http://127.0.0.1:17892; or exit 3
printf '1 2 0 127.0.0.1 17891 0\n' > %s/request
__lazyclash_sync
not set -q http_proxy; or exit 4
exit 0
`, hookQuote(script), hookQuote(dir), hookQuote(dir), hookQuote(dir)))
			} else {
				runHookShell(t, shell, fmt.Sprintf(`
. %s || exit 1
printf '1 2 127.0.0.1 17892\n' > %s/endpoint
__lazyclash_sync
[ "$http_proxy" = http://127.0.0.1:17892 ] || exit 2
rm %s/endpoint
__lazyclash_sync
[ "$http_proxy" = http://127.0.0.1:17892 ] || exit 3
printf '1 2 0 127.0.0.1 17891 0\n' > %s/request
__lazyclash_sync
[ "${http_proxy+x}" != x ] || exit 4
exit 0
`, hookQuote(script), hookQuote(dir), hookQuote(dir), hookQuote(dir)))
			}
		})
	}
}

func TestHooksUnsetVariablesWithNounset(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			script, _ := hookFixture(t, shell)
			runHookShell(t, shell, fmt.Sprintf("set -u\n. %s || exit 1\n[ \"$http_proxy\" = http://127.0.0.1:17891 ] || exit 2\nexit 0", hookQuote(script)))
		})
	}
}

func TestReadonlyPromptConfigurationDoesNotClaimLoaded(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			script, _ := hookFixture(t, shell)
			setup := "readonly PROMPT_COMMAND=': original'"
			if shell == "zsh" {
				setup = "typeset -ar precmd_functions=(original)"
			}
			runHookShell(t, shell, fmt.Sprintf("%s\n. %s && exit 1\n[ \"${LAZYCLASH_SHELL_LOADED+x}\" != x ] || exit 2\n[ \"${http_proxy+x}\" != x ] || exit 3\nexit 0", setup, hookQuote(script)))
		})
	}
}
