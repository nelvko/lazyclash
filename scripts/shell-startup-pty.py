#!/usr/bin/env python3
"""Verify installed Shell Integration in real Linux interactive startup chains.

Run the unpacked release installer directly in each supported shell.
All startup files, state, and config live beneath a fresh temporary HOME.
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
import sys
import tempfile
import time


class Shell:
    def __init__(self, argv, env, transcript):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execvpe(argv[0], argv, env)
        self.buffer = b""
        self.transcript = transcript
        self.closed = False

    def send(self, text):
        os.write(self.fd, (text + "\n").encode())

    def expect(self, pattern, timeout=15):
        expected = re.compile(pattern.encode())
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            found = expected.search(self.buffer)
            if found:
                self.buffer = self.buffer[found.end():]
                return
            ready, _, _ = select.select([self.fd], [], [], 0.1)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError as error:
                if error.errno == errno.EIO:
                    break
                raise
            if not data:
                break
            self.transcript.write(data)
            self.transcript.flush()
            self.buffer += data
        raise AssertionError(f"did not observe {pattern!r}: {self.buffer[-3000:]!r}")

    def close(self):
        if self.closed:
            return
        self.closed = True
        self.send("exit")
        deadline = time.monotonic() + 4
        while time.monotonic() < deadline:
            pid, _ = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                os.close(self.fd)
                return
            time.sleep(0.05)
        os.kill(self.pid, signal.SIGKILL)
        os.waitpid(self.pid, 0)
        os.close(self.fd)


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_text(value)
    path.chmod(0o600)


def run_scenario(name, shell_name, login_file, installer, output):
    with tempfile.TemporaryDirectory(prefix="lazyclash-startup-") as temporary:
        home = pathlib.Path(temporary).resolve()
        binary = home / ".local" / "bin" / "lazyclash"
        config, cache = home / ".config", home / ".cache"
        env = dict(os.environ)
        for key in tuple(env):
            if key.startswith("LAZYCLASH_"):
                env.pop(key)
        env.update(HOME=str(home), XDG_CONFIG_HOME=str(config),
                   XDG_CACHE_HOME=str(cache), TERM="xterm-256color",
                   http_proxy="http://existing.example:8080",
                   PS1="LC_READY> ", LC_ALL="C.UTF-8")
        expected_profile = "nonlogin"
        if shell_name == "bash":
            prompt = "__lc_user_prompt() { LC_USER_PROMPTS=$(( ${LC_USER_PROMPTS:-0} + 1 )); }; "
            if name == "bash-array":
                prompt += "PROMPT_COMMAND=( __lc_user_prompt )\n"
            else:
                prompt += "PROMPT_COMMAND=__lc_user_prompt\n"
            write(home / ".bashrc", prompt + "export LC_STARTUP=nonlogin\n")
            if login_file:
                expected_profile = login_file
                for path in (".profile", ".bash_login", ".bash_profile"):
                    write(home / path, prompt + f"export LC_STARTUP={shlex.quote(path)}\n")
                    if path == login_file:
                        break
            install_argv = ["bash", "--noprofile", "--norc", "-i"]
            startup_argv = ["bash", "-i"] if not login_file else ["bash", "--login", "-i"]
        elif shell_name == "zsh":
            dotdir = home / "custom-zdotdir"
            env["ZDOTDIR"] = str(dotdir)
            write(dotdir / ".zshrc", "autoload -Uz add-zsh-hook\n"
                  "__lc_user_prompt() { (( LC_USER_PROMPTS += 1 )); return 0; }\n"
                  "add-zsh-hook precmd __lc_user_prompt\n"
                  "PS1='LC_READY> '\nexport LC_STARTUP=zsh\n")
            install_argv = ["zsh", "-f", "-i"]
            startup_argv = ["zsh", "-i"]
            expected_profile = "zsh"
        else:
            write(config / "fish" / "config.fish",
                  "set -gx LC_STARTUP fish\n"
                  "set -g LC_USER_PROMPTS 0\n"
                  "function fish_prompt\n"
                  "  set -g LC_USER_PROMPTS (math \"$LC_USER_PROMPTS + 1\")\n"
                  "  printf 'LC_READY> '\nend\n")
            install_argv = ["fish", "--no-config", "-i"]
            startup_argv = ["fish", "-i"]
            expected_profile = "fish"

        with (output / f"{name}.ansi").open("wb") as transcript:
            installing = Shell(install_argv, env, transcript)
            try:
                installing.send("printf '\\nLC_INSTALL_READY\\n'")
                installing.expect(r"\r?\nLC_INSTALL_READY\r?\n")
                if name == "bash-array":
                    # Move from a standalone download to the installed binary.
                    download = home / "standalone-lazyclash"
                    shutil.copy2(installer.with_name("lazyclash"), download)
                    installing.send(shlex.quote(str(download)) + " shell install && printf '\\nLC_STANDALONE\\n'")
                    installing.expect(r"\r?\nLC_STANDALONE\r?\n")
                    download.unlink()
                installing.send(shlex.quote(str(installer)) + " && printf '\\nLC_INSTALLED\\n'")
                installing.expect(r"\r?\nLC_INSTALLED\r?\n")
                installing.send(shlex.quote(str(binary)) + " shell status --json")
                installing.expect(r'"state":"off","installed":true,"loaded":false[^\r\n]*"pending":false')
            finally:
                installing.close()

            started = Shell(startup_argv, env, transcript)
            try:
                if shell_name == "fish":
                    probe = "printf '\\nLC_START:%s:%s\\n' \"$LC_STARTUP\" \"$LAZYCLASH_SHELL_LOADED\""
                else:
                    probe = "printf '\\nLC_START:%s:%s\\n' \"${LC_STARTUP-}\" \"${LAZYCLASH_SHELL_LOADED-}\""
                started.send(probe)
                started.expect(r"\r?\nLC_START:" + re.escape(expected_profile) + r":1\r?\n")
                # A second roundtrip proves the existing prompt callback survives.
                started.send("printf '\\nLC_PROMPTS:%s\\n' \"$LC_USER_PROMPTS\"")
                started.expect(r"\r?\nLC_PROMPTS:[1-9][0-9]*\r?\n")
                started.send(shlex.quote(str(binary)) + " shell status --json")
                started.expect(r'"state":"off","installed":true,"loaded":true[^\r\n]*"pending":false')
                started.send("printf '\\nLC_ORIGINAL:%s\\n' \"$http_proxy\"")
                started.expect(r"\r?\nLC_ORIGINAL:http://existing.example:8080\r?\n")
            finally:
                started.close()
        return {"name": name, "shell": shell_name, "startup": startup_argv,
                "first_readable_login_file": login_file, "loaded": True,
                "existing_prompt_survived": True, "result": "passed"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--installer", type=pathlib.Path, required=True, help="install.sh from an unpacked Linux release")
    parser.add_argument("--output", type=pathlib.Path, required=True)
    args = parser.parse_args()
    if not sys.platform.startswith("linux"):
        parser.error("Linux controlling-PTY tests must run on Linux")
    installer = args.installer.resolve()
    args.output.mkdir(parents=True, exist_ok=True)
    results = []
    scenarios = [("bash-nonlogin", "bash", None), ("bash-array", "bash", None),
                 ("bash-profile", "bash", ".profile"),
                 ("bash-login", "bash", ".bash_login"),
                 ("bash-bash-profile", "bash", ".bash_profile"),
                 ("zsh-zdotdir", "zsh", None), ("fish-conf-d", "fish", None)]
    try:
        for name, shell, login in scenarios:
            if not shutil.which(shell):
                results.append({"name": name, "result": "skipped", "reason": f"{shell} missing"})
                continue
            result = run_scenario(name, shell, login, installer, args.output)
            results.append(result)
            print(json.dumps(result), flush=True)
    finally:
        (args.output / "summary.json").write_text(json.dumps(results, indent=2) + "\n")


if __name__ == "__main__":
    main()
