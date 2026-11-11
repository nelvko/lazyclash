#!/usr/bin/env python3
"""Exercise actual Shell Integration in Linux controlling PTYs.

The test helper only installs and writes requests via the shared Manager API;
_attach/_finalize/status are handled by the supplied production executable.
No real user startup files or proxy services are changed.
"""
import argparse
import errno
import json
import os
import pathlib
import pty
import re
import select
import shlex
import shutil
import signal
import tempfile
import time


class Shell:
    def __init__(self, name, env):
        self.name = name
        self.transcript = bytearray()
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            argv = {"bash": ["bash", "--noprofile", "--norc", "-i"],
                    "zsh": ["zsh", "-f", "-i"],
                    "fish": ["fish", "--no-config", "--interactive"]}[name]
            os.execvpe(name, argv, env)
        self.read_for(.2)

    def read_for(self, duration):
        end = time.monotonic() + duration
        result = bytearray()
        while time.monotonic() < end:
            ready, _, _ = select.select([self.fd], [], [], max(0, end-time.monotonic()))
            if not ready:
                break
            try:
                chunk = os.read(self.fd, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    break
                raise
            if not chunk:
                break
            result.extend(chunk)
            self.transcript.extend(chunk)
        return bytes(result)

    def command(self, text, expected=0, timeout=20):
        token = os.urandom(8).hex()
        status = "$status" if self.name == "fish" else "$?"
        marker = "LC_DONE_" + token
        os.write(self.fd, (text + "; printf '\\n" + marker + ":%s\\n' " + status + "\n").encode())
        result = bytearray()
        deadline = time.monotonic() + timeout
        pattern = re.compile((marker+r":(\d+)\r?\n").encode())
        while time.monotonic() < deadline:
            result.extend(self.read_for(.05))
            match = pattern.search(result)
            if match:
                if int(match[1]) != expected:
                    raise AssertionError(f"{self.name} command returned {match[1].decode()}: {text}\n{result.decode(errors='replace')}")
                return result.decode(errors="replace")
        raise AssertionError(f"{self.name} command timed out: {text}\n{result.decode(errors='replace')}")

    def close(self):
        try:
            os.write(self.fd, b"exit\n")
            deadline = time.monotonic()+3
            while time.monotonic()<deadline:
                pid, status = os.waitpid(self.pid, os.WNOHANG)
                if pid:
                    break
                self.read_for(.05)
            else:
                os.kill(self.pid, signal.SIGKILL)
                os.waitpid(self.pid, 0)
        finally:
            os.close(self.fd)


def env_for(root, binary):
    env = {key: value for key, value in os.environ.items()
           if key not in {"http_proxy", "https_proxy", "all_proxy", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "no_proxy", "NO_PROXY"}
           and not key.startswith("LAZYCLASH_SHELL_")}
    env.update(HOME=str(root), XDG_CONFIG_HOME=str(root/"config"), XDG_CACHE_HOME=str(root/"cache"),
               XDG_DATA_HOME=str(root/"data"), TERM="xterm-256color", PS1="LC> ",
               LAZYCLASH_SHELL_PTY_DRIVER="1", LAZYCLASH_TEST_SHELL_BINARY=str(binary))
    return env


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True, type=pathlib.Path)
    parser.add_argument("--helper", required=True, type=pathlib.Path)
    parser.add_argument("--output", type=pathlib.Path)
    args = parser.parse_args()
    binary, helper = args.binary.resolve(), args.helper.resolve()
    results = []
    for name in ("bash", "zsh", "fish"):
        if not shutil.which(name):
            raise RuntimeError(name+" is required for this acceptance matrix")
        with tempfile.TemporaryDirectory(prefix="lazyclash-shell-pty-") as directory:
            root = pathlib.Path(directory)
            env = env_for(root, binary)
            first = Shell(name, env)
            second = None
            q = shlex.quote
            join = "; and " if name == "fish" else " && "
            def assert_cmd(test):
                return ("test "+test)
            def action(shell, mode, port=17891):
                command = f"LAZYCLASH_SHELL_PTY_ACTION={mode} LAZYCLASH_SHELL_PTY_PORT={port} {q(str(helper))} -test.run='^TestShellPTYDriver$'"
                return shell.command(command)
            script = root/"config"/"lazyclash"/"shell"/("fish.fish" if name == "fish" else name+".sh")
            try:
                if name == "fish":
                    first.command("function fish_prompt; printf 'LC> '; end; function existing_prompt --on-event fish_prompt; set -g existing_hits (math 1 + 0$existing_hits); end; set -g existing_hits 0")
                    first.command("set -gu http_proxy 'original\nquote\"value'; set -gx HTTP_PROXY 'UPPER original'; set -gx https_proxy ''; set -eg all_proxy; set -eg ALL_PROXY; set -gx no_proxy 'old, localhost'; set -gx NO_PROXY 'UPPER,old'; true")
                elif name == "bash":
                    first.command("PROMPT_COMMAND=': existing_prompt'; http_proxy='original\nquote\"value'; export -n http_proxy; export HTTP_PROXY='UPPER original' https_proxy=''; unset all_proxy ALL_PROXY; export no_proxy='old, localhost' NO_PROXY='UPPER,old'")
                else:
                    first.command("existing_hits=0; existing_precmd() { (( existing_hits++ )); return 0; }; precmd_functions=(existing_precmd); http_proxy='original\nquote\"value'; typeset +x http_proxy; export HTTP_PROXY='UPPER original' https_proxy=''; unset all_proxy ALL_PROXY; export no_proxy='old, localhost' NO_PROXY='UPPER,old'")
                action(first, "install")
                action(first, "enable")
                # The check is in the same command as first source: no next prompt
                # and no second TUI/enable request may be needed.
                first.command("source "+q(str(script))+join+assert_cmd('"$http_proxy" = http://127.0.0.1:17891')+join+assert_cmd('"$all_proxy" = socks5h://127.0.0.1:17891'))
                first.command(q(str(binary))+" shell status")
                first.command("source "+q(str(script))+join+assert_cmd('"$no_proxy" = old,localhost,UPPER,127.0.0.1,::1'))
                # A second independent terminal shares installation and cache,
                # but must neither inherit nor claim terminal A's enabled intent.
                second = Shell(name, env)
                second.command("source "+q(str(script)))
                if name == "fish":
                    second.command("not set -q http_proxy")
                else:
                    second.command('[ "${http_proxy+x}" != x ]')
                action(second, "enable")
                second.command(assert_cmd('"$http_proxy" = http://127.0.0.1:17891'))
                action(first, "port", 17892)
                first.command(assert_cmd('"$http_proxy" = http://127.0.0.1:17892'))
                second.command("true")  # Run B's next prompt hook after endpoint publication.
                second.command(assert_cmd('"$http_proxy" = http://127.0.0.1:17892'))
                action(first, "disable", 17892)
                if name == "fish":
                    first.command(assert_cmd('"$http_proxy" = \'original\nquote"value\'')+join+"not set -qgx http_proxy"+join+assert_cmd('"$HTTP_PROXY" = \'UPPER original\'')+join+"set -qgx HTTP_PROXY"+join+"set -qgx https_proxy"+join+assert_cmd('"$https_proxy" = \'\'')+join+"not set -q all_proxy"+join+assert_cmd('"$no_proxy" = \'old, localhost\'')+join+assert_cmd('"$NO_PROXY" = UPPER,old'))
                else:
                    first.command(assert_cmd('"$http_proxy" = \'original\nquote"value\'')+join+assert_cmd('"$HTTP_PROXY" = \'UPPER original\'')+join+assert_cmd('"${all_proxy+x}" != x')+join+assert_cmd('"${https_proxy+x}" = x')+join+assert_cmd('"$https_proxy" = \'\'')+join+assert_cmd('"$no_proxy" = \'old, localhost\'')+join+assert_cmd('"$NO_PROXY" = UPPER,old'))
                    first.command("python3 -c 'import os; assert \"http_proxy\" not in os.environ; assert os.environ[\"HTTP_PROXY\"] == \"UPPER original\"; assert os.environ[\"https_proxy\"] == \"\"'")
                # A's off never alters B.
                second.command(assert_cmd('"$http_proxy" = http://127.0.0.1:17892'))
                action(second, "disable", 17892)
                if name == "fish": second.command("not set -q http_proxy")
                else: second.command('[ "${http_proxy+x}" != x ]')
                results.append({"shell": name, "first_source_immediate": True, "exact_restore": True,
                                "isolated_terminals": True, "port_update": True, "repeat_source": True})
            finally:
                if args.output:
                    args.output.mkdir(parents=True, exist_ok=True)
                    (args.output/(name+"-a.ansi")).write_bytes(first.transcript)
                    if second: (args.output/(name+"-b.ansi")).write_bytes(second.transcript)
                if second: second.close()
                first.close()
    print(json.dumps(results, ensure_ascii=False, indent=2))
    if args.output: (args.output/"summary.json").write_text(json.dumps(results, indent=2)+"\n")


if __name__ == "__main__":
    main()
