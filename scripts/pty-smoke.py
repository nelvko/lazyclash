#!/usr/bin/env python3
"""Exercise Bubble Tea through an actual controlling PTY, using stdlib only.

Run empty mode before installing, installed mode while the dedicated test service
is running. This is separate from Go model tests and never uses a real user home.
"""
import argparse
import codecs
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time
import unicodedata


ANSI = re.compile(r"\x1b\][^\x07]*(?:\x07|\x1b\\)|\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-_]")


class Screen:
    """Small VT screen for the cursor/erase operations used by Bubble Tea.

    Assertions read the current screen, so a cell renderer's changed characters
    are never mistaken for complete appended lines. This uses no host packages.
    """
    def __init__(self, columns, rows):
        self.columns, self.rows = columns, rows
        self.cells = [[" "] * columns for _ in range(rows)]
        self.x = self.y = 0
        self.top, self.bottom = 0, rows - 1
        self.saved = (0, 0)
        self.synchronized = False
        self.state, self.sequence = "text", ""
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")

    def resize(self, columns, rows):
        self.cells = [(line[:columns] + [" "] * columns)[:columns] for line in self.cells[:rows]]
        self.cells.extend([[" "] * columns for _ in range(rows - len(self.cells))])
        self.columns, self.rows = columns, rows
        self.x, self.y = min(self.x, columns - 1), min(self.y, rows - 1)
        self.top, self.bottom = 0, rows - 1

    def scroll(self, top, bottom, count):
        count = max(-(bottom - top + 1), min(count, bottom - top + 1))
        region = self.cells[top:bottom + 1]
        blank = [[" "] * self.columns for _ in range(abs(count))]
        self.cells[top:bottom + 1] = (region[count:] + blank) if count >= 0 else (blank + region[:count])

    def newline(self):
        if self.y == self.bottom:
            self.scroll(self.top, self.bottom, 1)
        else:
            self.y = min(self.rows - 1, self.y + 1)

    def feed(self, chunk):
        for char in self.decoder.decode(chunk):
            if self.state == "osc":
                if char == "\x07":
                    self.state = "text"
                elif char == "\x1b":
                    self.state = "osc-end"
                continue
            if self.state == "osc-end":
                self.state = "text" if char == "\\" else "osc"
                continue
            if self.state == "charset":
                self.state = "text"
                continue
            if self.state == "escape":
                self.state = "text"
                if char == "[":
                    self.state, self.sequence = "csi", ""
                elif char == "]":
                    self.state = "osc"
                elif char in "()":
                    self.state = "charset"
                elif char == "7":
                    self.saved = self.x, self.y
                elif char == "8":
                    self.x, self.y = self.saved
                elif char == "D":
                    self.newline()
                elif char == "M":
                    if self.y == self.top:
                        self.scroll(self.top, self.bottom, -1)
                    else:
                        self.y = max(0, self.y - 1)
                elif char == "E":
                    self.x = 0
                    self.newline()
                continue
            if self.state == "csi":
                if "@" <= char <= "~":
                    self.csi(char, self.sequence)
                    self.state = "text"
                else:
                    self.sequence += char
                continue
            if char == "\x1b":
                self.state = "escape"
            elif char == "\r":
                self.x = 0
            elif char == "\n":
                self.newline()
            elif char == "\b":
                self.x = max(0, self.x - 1)
            elif char == "\t":
                self.x = min(self.columns - 1, (self.x // 8 + 1) * 8)
            elif ord(char) >= 32 and char != "\x7f":
                if unicodedata.combining(char) or char in "\ufe0f\u200d":
                    if self.x > 0:
                        self.cells[self.y][min(self.x - 1, self.columns - 1)] += char
                    continue
                width = 2 if unicodedata.east_asian_width(char) in ("W", "F") else 1
                if self.x + width > self.columns:
                    self.x = 0
                    self.newline()
                self.cells[self.y][self.x] = char
                if width == 2:
                    self.cells[self.y][self.x + 1] = ""
                self.x += width

    def csi(self, final, sequence):
        if sequence.startswith(("?", ">", "<", "=")):
            if sequence == "?2026":
                self.synchronized = final == "h"
            if final == "h" and sequence in ("?1049", "?1047"):
                self.cells = [[" "] * self.columns for _ in range(self.rows)]
                self.x = self.y = 0
                self.top, self.bottom = 0, self.rows - 1
            return
        values = sequence.split(";")
        numbers = [int(value) if value.isdigit() else 0 for value in values]
        n = numbers[0] or 1
        if final in ("H", "f"):
            self.y = min(self.rows - 1, max(0, numbers[0] - 1))
            self.x = min(self.columns - 1, max(0, (numbers[1] if len(numbers) > 1 else 1) - 1))
        elif final == "A": self.y = max(0, self.y - n)
        elif final in ("B", "e"): self.y = min(self.rows - 1, self.y + n)
        elif final in ("C", "a"): self.x = min(self.columns - 1, self.x + n)
        elif final == "D": self.x = max(0, self.x - n)
        elif final == "E": self.y, self.x = min(self.rows - 1, self.y + n), 0
        elif final == "F": self.y, self.x = max(0, self.y - n), 0
        elif final in ("G", "`"): self.x = min(self.columns - 1, n - 1)
        elif final == "d": self.y = min(self.rows - 1, n - 1)
        elif final == "Z": self.x = max(0, ((min(self.x, self.columns - 1) - 1) // 8 - (n - 1)) * 8)
        elif final == "J":
            mode = numbers[0]
            if mode in (2, 3):
                self.cells = [[" "] * self.columns for _ in range(self.rows)]
            elif mode == 0:
                self.cells[self.y][self.x:] = [" "] * (self.columns - self.x)
                for row in range(self.y + 1, self.rows): self.cells[row] = [" "] * self.columns
            elif mode == 1:
                for row in range(self.y): self.cells[row] = [" "] * self.columns
                self.cells[self.y][:self.x + 1] = [" "] * (self.x + 1)
        elif final == "K":
            mode = numbers[0]
            start, end = (0, self.columns) if mode == 2 else ((0, self.x + 1) if mode == 1 else (self.x, self.columns))
            self.cells[self.y][start:end] = [" "] * (end - start)
        elif final == "X":
            end = min(self.columns, self.x + n)
            self.cells[self.y][self.x:end] = [" "] * (end - self.x)
        elif final == "P":
            self.cells[self.y] = (self.cells[self.y][:self.x] + self.cells[self.y][self.x + n:] + [" "] * n)[:self.columns]
        elif final == "@":
            self.cells[self.y] = (self.cells[self.y][:self.x] + [" "] * n + self.cells[self.y][self.x:])[:self.columns]
        elif final in ("L", "M"):
            if self.top <= self.y <= self.bottom:
                self.scroll(self.y, self.bottom, -n if final == "L" else n)
        elif final in ("S", "T"):
            self.scroll(self.top, self.bottom, n if final == "S" else -n)
        elif final == "r":
            top = (numbers[0] or 1) - 1
            bottom = (numbers[1] if len(numbers) > 1 and numbers[1] else self.rows) - 1
            if 0 <= top < bottom < self.rows:
                self.top, self.bottom = top, bottom
                self.x = self.y = 0
        elif final == "s": self.saved = self.x, self.y
        elif final == "u": self.x, self.y = self.saved

    def text(self):
        return "\n".join("".join(line).rstrip() for line in self.cells)


class Terminal:
    def __init__(self, command):
        self.raw = bytearray()
        self.screen = Screen(120, 40)
        self.closed = False
        self.status = None
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
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
        self.screen.feed(chunk)
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
            if not self.screen.synchronized and needle in self.screen.text() and (not after or len(self.text()) > after):
                return
            self.read()
            if self.closed:
                break
        raise AssertionError("PTY did not render expected text: " + repr(needle) + "\nCurrent terminal screen:\n" + self.screen.text())

    def wait_absent(self, needle, timeout=10):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if not self.screen.synchronized and needle not in self.screen.text():
                return
            self.read()
        raise AssertionError("PTY did not close expected screen: " + repr(needle) + "\n" + self.screen.text())

    def resize(self, columns, rows):
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
        self.screen.resize(columns, rows)
        os.kill(self.pid, signal.SIGWINCH)

    def key(self, data):
        self.send(data)
        self.read(0.08)

    def local_confirm(self, source):
        self.key(b"b")
        self.wait("Local Mihomo executable")
        self.key(os.fsencode(source))
        self.key(b"\t")  # cancel
        self.key(b"\t")  # continue
        mark = len(self.text())
        self.key(b"\r")
        self.wait("Review installation", after=mark)

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
    parser.add_argument("--mode", choices=["empty", "installed", "install-local", "runtime"], required=True)
    parser.add_argument("--mihomo-bin")
    parser.add_argument("--connection-id", help="Dedicated active fixture connection UUID for runtime mode")
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
            terminal.wait("Install your first proxy core")
            terminal.key(b"a")
            terminal.wait("Configuration name")
            terminal.key(b"\x1b")
            terminal.wait_absent("Import configuration")
            terminal.wait("Input canceled")

            # Network inputs are independently editable and remain hidden.
            # Esc discards the complete draft without persisting credentials.
            terminal.key(b"n")
            terminal.wait("Network settings")
            terminal.key(b"\r")  # official -> prefix
            terminal.wait("Prefix URL")
            terminal.key(b"\t")
            terminal.key(b"https://accelerator.example.invalid/private-path-token")
            terminal.key(b"\t")  # proxy mode
            terminal.key(b"\r")  # environment
            terminal.key(b"\r")  # custom
            terminal.wait("Proxy URL")
            terminal.key(b"\t")
            terminal.key(b"http://user:private-proxy-token@127.0.0.1:18080")
            if "private-path-token" in terminal.text() or "private-proxy-token" in terminal.text():
                raise AssertionError("network credentials appeared in the terminal")
            terminal.key(b"\x1b")
            terminal.wait_absent("Mihomo · Source + Connection")

            # Import works before installing a core and remains explicitly
            # unvalidated by the engine until a later successful application.
            config = home.parent / "preimport.yaml"
            config.write_text("proxies: []\nrules: ['MATCH,DIRECT']\n")
            terminal.key(b"a")
            terminal.wait("Configuration name")
            terminal.key(b"preimport\r")
            terminal.wait("Subscription URL or local YAML path")
            terminal.key(os.fsencode(config) + b"\r")
            terminal.wait("Configuration saved")
            if not (home / "profiles" / "preimport.yaml").exists():
                raise AssertionError("pre-install import was not saved")

            terminal.key(b"i")
            terminal.resize(24, 8)
            terminal.wait("Please enlarge terminal")
            terminal.resize(80, 24)
            terminal.wait("Mihomo · Core")
            terminal.key(b"\x1b")
            terminal.wait_absent("Mihomo · Core")
            terminal.resize(120, 40)

            # First two version checks pass (preflight and recheck), while the
            # staged executable's third -v blocks inside the real install job.
            slow = home.parent / ("slow-" + args.service_name)
            counter = home.parent / ("counter-" + args.service_name)
            slow.write_text("#!/usr/bin/python3\nfrom pathlib import Path\nimport sys,time\np=Path(" + repr(str(counter)) + ")\nif '-v' in sys.argv:\n n=int(p.read_text())+1 if p.exists() else 1\n p.write_text(str(n))\n if n>=3: time.sleep(60)\n print('Mihomo Meta v1.19.33 linux arm64')\n")
            slow.chmod(0o700)
            terminal.local_confirm(slow)
            mark = len(terminal.text())
            terminal.key(b"\r")  # confirm install
            terminal.wait("Checking executable", after=mark)
            terminal.key(b"\x1b")
            terminal.wait("Installation canceled; cleanup completed", after=mark)
            if (home / "installation").exists():
                raise AssertionError("canceled install left a half installation")
            terminal.exit(b"\x03")
            # SIGTERM must propagate through the application's cancellation
            # context and let rollback finish before Bubble Tea tears down.
            signal_terminal = Terminal(command)
            terminals.append(signal_terminal)
            signal_terminal.wait("State refreshed")
            counter.unlink()
            signal_terminal.local_confirm(slow)
            signal_terminal.key(b"\r")
            signal_terminal.wait("Checking executable")
            os.kill(signal_terminal.pid, signal.SIGTERM)
            signal_terminal.wait_exit()
            if (home / "installation").exists() or list(home.glob(".install-*")) or list(home.glob(".core-stage-*")):
                raise AssertionError("SIGTERM left a half installation or staging directory")
            slow.unlink()
            counter.unlink()
        elif args.mode == "install-local":
            if not args.mihomo_bin:
                parser.error("--mihomo-bin is required for install-local mode")
            terminal.local_confirm(args.mihomo_bin)
            mark = len(terminal.text())
            terminal.key(b"\r")
            terminal.wait("Mihomo installed and verified", after=mark)
            terminal.wait(str(home / "installation" / "mihomo"), after=mark)
            terminal.wait("Service remains stopped", after=mark)
            terminal.key(b"\r")
            terminal.exit()
        elif args.mode == "runtime":
            if not args.connection_id:
                parser.error("--connection-id is required for runtime mode")

            def cli(*arguments):
                completed = subprocess.run(command + list(arguments), stdout=subprocess.PIPE,
                                           stderr=subprocess.PIPE, timeout=20)
                if completed.returncode:
                    raise AssertionError("Dedicated test CLI action failed: " + " ".join(arguments[:2]))
                return completed.stdout

            terminal.wait("Since core start")
            terminal.wait("Live")
            terminal.key(b"5")
            terminal.wait("Active connections")
            terminal.wait("Live")
            terminal.key(b"/")
            terminal.wait("Search connections")
            terminal.key(args.connection_id.encode("ascii"))
            terminal.key(b"\r")  # finish search and focus its exact match
            terminal.key(b"\r")
            terminal.wait("Connection details")
            terminal.wait(args.connection_id)
            terminal.resize(60, 20)
            terminal.wait("Connection details")
            terminal.resize(120, 40)
            terminal.key(b"x")
            terminal.wait("Close this connection?")
            terminal.key(b"y")
            terminal.wait("Connection close requested")
            current = json.loads(cli("connections"))
            if any(item["id"] == args.connection_id for item in (current["connections"] or [])):
                raise AssertionError("TUI reported close success but the real connection still exists")
            # Closing the final fixture connection must retain already counted
            # bytes; the active list is not a substitute for cumulative totals.
            closed = json.loads(cli("stats"))
            if closed["Connections"]["uploadTotal"] <= 0 or closed["Connections"]["downloadTotal"] <= 0:
                raise AssertionError("Closing connections lost their cumulative bytes")

            terminal.key(b"1")
            terminal.wait("Traffic statistics")
            cli("service", "stop")
            terminal.wait("Stopped")
            stopped = json.loads(cli("stats"))
            if stopped["State"] != "stopped" or stopped["Traffic"] is not None:
                raise AssertionError("Stopped core retained live telemetry")
            cli("service", "start")
            terminal.wait("Live")
            terminal.wait("Since core start")
            terminal.exit()
        else:
            terminal.wait("Running")
            # SGR mouse press/release on the sidebar Nodes entry.
            terminal.key(b"\x1b[<0;5;7M\x1b[<0;5;7m")
            terminal.wait("Proxy groups and nodes")
            terminal.wait("PROXY")
            terminal.key(b"\x1b[Z")  # nav.nodes -> nav.overview
            terminal.key(b"\x1b[Z")  # nav.overview -> PROXY group
            terminal.key(b"\r")
            terminal.wait("REJECT")
            terminal.key(b"\t")  # DIRECT
            terminal.key(b"\t")  # REJECT
            terminal.key(b"\r")
            terminal.wait("Node selected")
            terminal.wait("PROXY [Selector] → REJECT")
            terminal.key(b"3")
            terminal.wait("default")
            terminal.resize(20, 5)
            time.sleep(0.1)
            terminal.resize(120, 40)
            terminal.key(b"1")
            terminal.wait("Running")
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
