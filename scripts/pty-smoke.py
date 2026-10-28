#!/usr/bin/env python3
"""Exercise Bubble Tea through an actual controlling PTY, using stdlib only.

Run empty mode before installing, installed mode while the dedicated test service
is running. This is separate from Go model tests and never uses a real user home.
"""
import argparse
import errno
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import sys
import termios
import time


ANSI = re.compile(r"\x1b\][^\x07]*(?:\x07|\x1b\\)|\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-_]")


class Terminal:
    def __init__(self, command):
        self.raw = bytearray()
        self.closed = False
        self.status = None
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 28, 100, 0, 0))
            env = os.environ.copy()
            env.update(TERM="xterm-256color", NO_COLOR="1")
            os.execvpe(command[0], command, env)
        os.set_blocking(self.fd, False)

    def read(self, timeout=0.1):
        if self.closed or not select.select([self.fd], [], [], timeout)[0]:
            return
        try:
            chunk = os.read(self.fd, 65536)
        except OSError as exc:
            if exc.errno == errno.EIO:
                self.closed = True
                return
            raise
        if not chunk:
            self.closed = True
            return
        self.raw.extend(chunk)
        # Answer the terminal queries which can be issued during startup.
        if b"\x1b[6n" in chunk:
            self.send(b"\x1b[1;1R")
        if b"\x1b]11;?" in chunk:
            self.send(b"\x1b]11;rgb:0000/0000/0000\x07")

    def text(self):
        return ANSI.sub("", self.raw.decode("utf-8", errors="replace"))

    def send(self, data):
        os.write(self.fd, data)

    def wait(self, needle, timeout=20, after=0):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if needle in self.text()[after:]:
                return
            self.read()
            if self.closed:
                break
        raise AssertionError("PTY did not render expected text: " + repr(needle))

    def resize(self, columns, rows):
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
        os.kill(self.pid, signal.SIGWINCH)

    def exit(self, key=b"q"):
        self.send(key)
        self.wait_exit()

    def wait_exit(self):
        end = time.monotonic() + 15
        while time.monotonic() < end:
            self.read()
            got, status = os.waitpid(self.pid, os.WNOHANG)
            if got:
                self.status = status
                if os.waitstatus_to_exitcode(status) != 0:
                    raise AssertionError("TUI exited unsuccessfully")
                return
        raise AssertionError("TUI failed to exit and clean up within 15 seconds")

    def cleanup(self):
        # The PTY child created its own session. Also reap any of its children
        # after an abrupt parent exit so a failed cancellation test cannot leak.
        try:
            os.killpg(self.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        if self.status is None:
            try:
                os.waitpid(self.pid, 0)
            except ChildProcessError:
                pass
        os.close(self.fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--home", required=True)
    parser.add_argument("--service-name", required=True)
    parser.add_argument("--mode", choices=["empty", "installed"], required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    home = Path(args.home)
    if not home.is_absolute():
        parser.error("--home must be an absolute, dedicated temporary path")
    command = [args.binary, "--home", args.home, "--service-name", args.service_name]
    terminal = Terminal(command)
    terminals = [terminal]
    try:
        terminal.wait("State refreshed")
        if args.mode == "empty":
            terminal.wait("Mihomo is not installed")
            terminal.send(b"b")
            terminal.wait("Local Mihomo binary")
            terminal.send(b"\x1b")
            terminal.wait("Input canceled")
            terminal.resize(24, 8)
            terminal.wait("lazyclash")
            terminal.resize(100, 28)
            refreshed = len(terminal.text())
            terminal.send(b"g")
            terminal.wait("State refreshed", after=refreshed)
            # A controlled executable blocks in -v, giving cancellation a
            # deterministic real subprocess to terminate before unit creation.
            slow = home.parent / ("slow-" + args.service_name)
            slow.write_text("#!/usr/bin/python3\nimport time\ntime.sleep(60)\n")
            slow.chmod(0o700)
            terminal.send(b"b")
            start = len(terminal.text())
            terminal.wait("Local Mihomo binary", after=start)
            terminal.send(os.fsencode(slow) + b"\r")
            terminal.wait("Copying and validating", after=start)
            terminal.send(b"\x1b")
            terminal.wait("context canceled", after=start)
            if (home / "installation").exists():
                raise AssertionError("canceled install left a half installation")
            terminal.exit(b"\x03")
            # SIGTERM must propagate through the application's cancellation
            # context and let rollback finish before Bubble Tea tears down.
            signal_terminal = Terminal(command)
            terminals.append(signal_terminal)
            signal_terminal.wait("State refreshed")
            signal_terminal.send(b"b")
            signal_terminal.wait("Local Mihomo binary")
            signal_terminal.send(os.fsencode(slow) + b"\r")
            signal_terminal.wait("Copying and validating")
            os.kill(signal_terminal.pid, signal.SIGTERM)
            signal_terminal.wait_exit()
            if (home / "installation").exists() or list(home.glob(".install-*")):
                raise AssertionError("SIGTERM left a half installation or staging directory")
            slow.unlink()
        else:
            terminal.wait("Service: active")
            terminal.send(b"3")
            terminal.wait("PROXY")
            # Default Mihomo exposes GLOBAL then PROXY. Select PROXY's REJECT
            # child, which differs from its initial DIRECT selection.
            terminal.send(b"j\r")
            terminal.wait("REJECT")
            # Arrow sequences remain distinct key events even when the PTY
            # delivers both in one read (adjacent runes can be one paste event).
            terminal.send(b"\x1b[B\x1b[B\r")
            terminal.wait("Node selected")
            terminal.wait("PROXY [Selector] → REJECT")
            terminal.send(b"2")
            terminal.wait("default")
            terminal.resize(20, 5)
            time.sleep(0.1)
            terminal.resize(100, 28)
            terminal.send(b"1")
            terminal.wait("Service: active")
            terminal.exit()
        print("Real PTY scenarios passed: " + args.mode)
    finally:
        Path(args.output).parent.mkdir(parents=True, exist_ok=True)
        # The exercised screens contain only application diagnostics and our
        # credential-free fixture names; no raw config, manifests or journal.
        Path(args.output).write_text("\n--- next PTY session ---\n".join(t.text() for t in terminals))
        for item in terminals:
            item.cleanup()


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, OSError) as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
