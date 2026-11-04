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
            env.update(TERM="xterm-256color", NO_COLOR="1", LAZYCLASH_LANG="en")
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

    def wait_selected_proxy(self, name, timeout=20):
        selected = re.compile(r"▌[ \t]*(?:›[ \t]*)?" + re.escape(name) + r"(?=[ \t]{2,}|$)")
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if not self.screen.synchronized and any(selected.search(line) for line in self.screen.text().splitlines()):
                return
            self.read()
            if self.closed:
                break
        raise AssertionError("PTY did not confirm selected proxy: " + repr(name) + "\n" + self.screen.text())

    def resize(self, columns, rows):
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
        self.screen.resize(columns, rows)
        os.kill(self.pid, signal.SIGWINCH)

    def key(self, data):
        self.send(data)
        self.read(0.08)

    def settle(self, timeout=4):
        """Drain complete frames before inspecting an input/focus change."""
        deadline, quiet = time.monotonic() + timeout, 0
        while time.monotonic() < deadline:
            before = len(self.raw)
            self.read(0.03)
            complete = not self.screen.synchronized and self.screen.state == "text"
            quiet = quiet + 1 if len(self.raw) == before and complete else 0
            if quiet == 2:
                return
        raise AssertionError("PTY did not finish a frame\n" + self.screen.text())

    def click_text(self, label):
        self.wait(label)
        for y, row in enumerate(self.screen.cells):
            for x, cell in enumerate(row):
                if cell and "".join(row[x:]).startswith(label):
                    self.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())
                    return
        raise AssertionError("Current PTY screen has no clickable text: " + label)

    def local_ready(self, source):
        # The initial footer may paint before the asynchronous state read ends.
        # Wait for the ready preparation view before sending its local shortcut.
        self.wait("Install the Mihomo core")
        self.key(b"1")  # Explicitly enter Overview content from navigation.
        self.key(b"b")
        self.wait("Local Mihomo executable")
        self.key(os.fsencode(source))
        # Typing a long path produces multiple incremental frames. Waiting for
        # its visible suffix and draining them prevents later Tab checks from
        # inspecting old input frames, especially with notifications present.
        self.wait(os.fsdecode(source)[-24:])
        self.settle()
        # The local source shortcut opens the inline options. Follow real Tab
        # navigation so the viewport reveals the focused installation action.
        for _ in range(16):
            self.key(b"\t")
            self.settle()
            if re.search(r"›\s*(?:\[\s*)?Install(?:\s*\])?", self.screen.text()):
                return
        raise AssertionError("Local installation action is not keyboard accessible\n" + self.screen.text())

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
    parser.add_argument("--mode", choices=["empty", "download-network", "installed", "install-local", "runtime", "rules", "logs", "runtime-settings"], required=True)
    parser.add_argument("--download-method", choices=["direct", "environment"], default="direct",
                        help="Explicit real download route for download-network mode; never retries another route")
    parser.add_argument("--mihomo-bin")
    parser.add_argument("--fixture-port", type=int, help="Dedicated loopback HTTP fixture port for live log events")
    parser.add_argument("--connection-id", help="Dedicated active fixture connection UUID for runtime mode")
    parser.add_argument("--mixed-port", type=int, help="Dedicated free replacement proxy port for runtime-settings mode")
    parser.add_argument("--controller-port", type=int, help="Dedicated free replacement API port for runtime-settings mode")
    parser.add_argument("--conflict-port", type=int, help="Occupied dedicated TCP fixture port for runtime-settings mode")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    home = Path(args.home)
    if not home.is_absolute():
        parser.error("--home must be an absolute, dedicated temporary path")
    command = [args.binary, "--home", args.home, "--service-name", args.service_name]
    terminal = Terminal(command)
    terminals = [terminal]
    try:
        terminal.wait("Overview")
        terminal.wait_absent("Loading state")
        terminal.wait("q Quit")
        if args.mode == "empty":
            terminal.wait("Install the Mihomo core")
            terminal.key(b"a")
            terminal.wait("Subscription URL or local YAML path")
            terminal.key(b"\x1b")
            terminal.wait_absent("Import configuration")
            terminal.wait("Input canceled")

            # Download method is one choice. The acceleration prefix is visible
            # and does not permit embedded credentials; Esc discards the draft.
            terminal.key(b"n")
            terminal.wait("Download method")
            for label in ("Direct", "Use environment proxy", "GitHub acceleration"):
                terminal.wait(label)
            terminal.key(b"\r")  # direct -> environment
            terminal.key(b"\r")  # environment -> accelerated
            terminal.wait("Acceleration URL")
            terminal.key(b"\t")
            terminal.key(b"https://accelerator.example.invalid/")
            terminal.wait("https://accelerator.example.invalid/")
            terminal.key(b"\x1b")
            terminal.wait_absent("Acceleration URL")

            # Import works before installing a core and remains explicitly
            # unvalidated by the engine until a later successful application.
            config = home.parent / "preimport.yaml"
            config.write_text("proxies: []\nrules: ['MATCH,DIRECT']\n")
            terminal.key(b"a")
            terminal.wait("Subscription URL or local YAML path")
            terminal.key(os.fsencode(config) + b"\r")
            terminal.wait("Configuration saved")
            imported = subprocess.run(command + ["profile", "show", "preimport"],
                                      check=True, capture_output=True, timeout=15)
            saved = json.loads(imported.stdout)
            if saved["name"] != "preimport" or saved["validated"]:
                raise AssertionError("pre-install import was not saved as awaiting engine validation")

            terminal.key(b"i")
            terminal.resize(24, 8)
            terminal.wait("Please enlarge terminal")
            terminal.resize(80, 24)
            terminal.wait("Download and install")
            terminal.key(b"\x1b")
            terminal.wait("q Quit")
            terminal.resize(120, 40)

            # First two version checks pass (preflight and recheck), while the
            # staged executable's third -v blocks inside the real install job.
            slow = home.parent / ("slow-" + args.service_name)
            counter = home.parent / ("counter-" + args.service_name)
            slow.write_text("#!/usr/bin/python3\nfrom pathlib import Path\nimport sys,time\np=Path(" + repr(str(counter)) + ")\nif '-v' in sys.argv:\n n=int(p.read_text())+1 if p.exists() else 1\n p.write_text(str(n))\n if n>=3: time.sleep(60)\n print('Mihomo Meta v1.19.33 linux arm64')\n")
            slow.chmod(0o700)
            terminal.local_ready(slow)
            mark = len(terminal.text())
            terminal.key(b"\r")  # preflight and install, without another page
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
            signal_terminal.wait("Overview")
            signal_terminal.wait_absent("Loading state")
            signal_terminal.wait("q Quit")
            counter.unlink()
            signal_terminal.local_ready(slow)
            signal_terminal.key(b"\r")
            signal_terminal.wait("Checking executable")
            os.kill(signal_terminal.pid, signal.SIGTERM)
            signal_terminal.wait_exit()
            if (home / "installation").exists() or list(home.glob(".install-*")) or list(home.glob(".core-stage-*")):
                raise AssertionError("SIGTERM left a half installation or staging directory")
            slow.unlink()
            counter.unlink()
        elif args.mode == "download-network":
            terminal.wait("Install the Mihomo core")
            terminal.key(b"n")  # Cancel the automatic lookup and open its draft.
            terminal.wait("Download method")
            terminal.wait("Package connection: Not tested")
            if args.download_method == "environment":
                terminal.key(b"\r")  # Direct -> explicit environment proxy.
                terminal.wait("(*) Use environment proxy")
            else:
                terminal.wait("(*) Direct")
            terminal.click_text("Test connection")
            terminal.wait("Version information: Passed", timeout=90)
            terminal.wait("Package connection: Passed", timeout=90)
            terminal.settle()
            if "Package connection: Failed" in terminal.screen.text():
                raise AssertionError("Real remote package connection failed")
            # Testing a draft may query version.txt and probe an official file,
            # but cannot deploy files, install a unit or start the core.
            if (home / "installation").exists() or list(home.glob(".install-*")) or list(home.glob(".core-stage-*")):
                raise AssertionError("Connection testing created installation files")
            terminal.key(b"\x1b")
            terminal.wait_absent("Package connection")
            terminal.exit()
        elif args.mode == "install-local":
            if not args.mihomo_bin:
                parser.error("--mihomo-bin is required for install-local mode")
            terminal.local_ready(args.mihomo_bin)
            mark = len(terminal.text())
            terminal.key(b"\r")
            terminal.wait("is installed and verified", after=mark)
            terminal.wait("Built-in default configuration is ready")
            terminal.wait("Start core")
            terminal.exit()
        elif args.mode == "runtime-settings":
            if not all(value and 1024 <= value <= 65535 for value in
                       (args.mixed_port, args.controller_port, args.conflict_port, args.fixture_port)):
                parser.error("runtime-settings requires explicit unprivileged test/fixture ports")

            def cli(*arguments):
                completed = subprocess.run(command + list(arguments), stdout=subprocess.PIPE,
                                           stderr=subprocess.PIPE, timeout=60)
                if completed.returncode:
                    raise AssertionError("Dedicated settings-test CLI action failed: " + " ".join(arguments[:2]))
                return completed.stdout

            initial = json.loads(cli("settings", "show"))
            initial_status = json.loads(cli("status"))
            initial_pointer = os.readlink(home / "installation" / "current")

            def open_ports():
                terminal.key(b"7")
                terminal.wait("Runtime settings / Ports")
                terminal.click_text("Runtime settings / Ports")
                terminal.wait("Edit ports")
                terminal.wait("Proxy port")

            def replace_input(value):
                terminal.key(b"\x05\x15" + str(value).encode("ascii"))

            open_ports()
            replace_input("123q7")
            terminal.wait("123q7")
            terminal.wait("Edit ports")
            terminal.key(b"\x1b")
            terminal.wait_absent("Edit ports")
            if json.loads(cli("settings", "show")) != initial or os.readlink(home / "installation" / "current") != initial_pointer:
                raise AssertionError("Canceling the runtime settings draft changed the installation")

            open_ports()
            replace_input(args.conflict_port)
            terminal.key(b"\t")  # controller input
            replace_input(args.controller_port)
            terminal.key(b"\r")  # asynchronous review from the input
            terminal.wait("unavailable")
            if json.loads(cli("status"))["Service"]["PID"] != initial_status["Service"]["PID"]:
                raise AssertionError("TUI conflict precheck stopped the running core")
            if json.loads(cli("settings", "show")) != initial:
                raise AssertionError("TUI conflict precheck changed runtime settings")
            # Failed preflight returns focus to the first editable field.
            # Do not assume the controller retains the focus it had at submit.
            replace_input(args.mixed_port)
            terminal.wait(str(args.mixed_port))
            terminal.wait_absent("unavailable")
            terminal.key(b"\t")
            for columns, rows in ((120, 32), (80, 24), (60, 20), (120, 18), (120, 40)):
                mark = len(terminal.text())
                terminal.resize(columns, rows)
                terminal.wait("Control API port", after=mark)
                terminal.wait(str(args.controller_port))
            terminal.key(b"\r")
            terminal.wait("Review changes")
            terminal.wait("Apply & restart")
            terminal.wait("active connections will be interrupted")
            if json.loads(cli("settings", "show")) != initial:
                raise AssertionError("Reviewing runtime settings applied them before confirmation")
            terminal.key(b"\r")
            terminal.wait("Runtime settings applied")
            terminal.wait(str(args.controller_port))
            terminal.key(b"\r")
            terminal.wait_absent("Back to Settings")
            expected = {"mixed_port": args.mixed_port, "controller_port": args.controller_port}
            if json.loads(cli("settings", "show")) != expected:
                raise AssertionError("TUI did not commit the reviewed runtime ports")

            # Every major API consumer must use the new authenticated endpoint.
            terminal.key(b"2")
            terminal.wait("Search groups or nodes")
            terminal.wait("PROXY")
            terminal.key(b"5")
            terminal.wait("Rules loaded")
            terminal.key(b"6")
            terminal.wait("live events")
            from http.client import HTTPConnection
            for _ in range(10):
                connection = HTTPConnection("127.0.0.1", args.mixed_port, timeout=5)
                try:
                    connection.request("GET", "http://127.0.0.1:%d/runtime-settings" % args.fixture_port)
                    response = connection.getresponse()
                    if response.status != 200:
                        raise AssertionError("New TUI proxy port could not reach its dedicated fixture")
                    response.read()
                finally:
                    connection.close()
                terminal.read(.15)
                if "[TCP]" in terminal.screen.text():
                    break
            terminal.wait("[TCP]")
            terminal.key(b"1")
            terminal.wait("Traffic statistics")
            terminal.wait("Live")
            terminal.wait("Mihomo · Running")
            terminal.exit()
        elif args.mode == "rules":
            def cli(*arguments, success=True):
                completed = subprocess.run(command + list(arguments), stdout=subprocess.PIPE,
                                           stderr=subprocess.PIPE, timeout=20)
                if (completed.returncode == 0) != success:
                    raise AssertionError("Dedicated rule-test CLI action returned unexpected status: " + " ".join(arguments[:2]))
                return completed.stdout

            terminal.key(b"5")
            terminal.wait("Effective rules")
            terminal.wait("Rules loaded")
            terminal.key(b"/")
            terminal.wait("Search rules")
            terminal.key(b"needle.example.invalid")
            terminal.key(b"\r")
            terminal.key(b"\r")
            terminal.wait("Rule details")
            terminal.wait("needle.example.invalid")
            terminal.wait("REJECT")
            terminal.resize(60, 20)
            terminal.wait("Rule details")
            terminal.resize(120, 40)
            terminal.key(b"\x1b")
            terminal.wait_absent("Rule details")
            terminal.key(b"g")
            terminal.wait("Rules loaded")

            # Clear the query before checking that unavailable data is hidden:
            # a remembered search value is not a stale effective-rule row.
            terminal.key(b"/")
            terminal.wait("Search rules")
            terminal.key(b"\x05\x15")  # Ctrl+E, Ctrl+U
            terminal.key(b"\r")
            terminal.wait("needle.example.invalid")
            cli("service", "stop")
            cli("rules", success=False)
            terminal.key(b"g")
            terminal.wait("Core stopped")
            terminal.wait_absent("needle.example.invalid")
            cli("service", "start")
            terminal.key(b"g")
            terminal.wait("Rules loaded")
            terminal.wait("needle.example.invalid")
            terminal.exit()
        elif args.mode == "logs":
            if not args.fixture_port or not 1024 <= args.fixture_port <= 65535:
                parser.error("--fixture-port must be the dedicated loopback fixture's unprivileged port")

            def cli(*arguments, success=True):
                completed = subprocess.run(command + list(arguments), stdout=subprocess.PIPE,
                                           stderr=subprocess.PIPE, timeout=20)
                if (completed.returncode == 0) != success:
                    raise AssertionError("Dedicated log-test CLI action returned unexpected status: " + " ".join(arguments[:2]))
                return completed.stdout

            def log_event():
                # Explicit HTTP proxy traffic to our own fixture emits a real
                # Mihomo TCP event; no external DNS or private URL is involved.
                from http.client import HTTPConnection
                connection = HTTPConnection("127.0.0.1", 7897, timeout=5)
                try:
                    connection.connect()
                    source_port = connection.sock.getsockname()[1]
                    connection.request("GET", "http://127.0.0.1:%d/log-event" % args.fixture_port)
                    response = connection.getresponse()
                    if response.status != 200:
                        raise AssertionError("Dedicated log traffic fixture returned an unexpected status")
                    response.read()
                    return source_port
                finally:
                    connection.close()

            terminal.key(b"6")
            terminal.wait("Mihomo logs")
            terminal.wait("live events")
            # Establishment is asynchronous; repeat only our local fixture event
            # until it has actually been observed through the API log stream.
            for _ in range(10):
                log_event()
                terminal.read(0.15)
                if "[TCP]" in terminal.screen.text():
                    break
            terminal.wait("[TCP]")
            # Numeric page navigation already enters the page content.
            terminal.key(b"f")
            terminal.wait("Paused")
            mark = len(terminal.text())
            terminal.resize(60, 20)
            terminal.wait("Mihomo logs", after=mark)
            mark = len(terminal.text())
            terminal.resize(120, 40)
            terminal.wait("Paused", after=mark)

            terminal.key(b"/")
            terminal.wait("Search retained entries · Enter / Esc finish")
            terminal.key(b"123q7")
            terminal.wait("123q7")
            terminal.wait("Mihomo logs")
            terminal.key(b"\x05\x15")
            terminal.key(b"\r")
            terminal.wait_absent("Search retained entries · Enter / Esc finish")
            terminal.wait("[TCP]")
            terminal.key(b"?")
            terminal.wait("Current page: Logs")
            terminal.key(b"1234567")
            terminal.wait("Current page: Logs")
            terminal.key(b"\x1b")
            terminal.wait_absent("Current page: Logs")
            terminal.key(b"\x1b[F")  # End: reconnect and follow newest entries
            terminal.wait("Capturing live events")
            terminal.wait("Capture resumed")

            cli("service", "stop")
            cli("logs", "--lines", "1", "--duration", "1s", success=False)
            terminal.wait("Capture unavailable")
            stopped = json.loads(cli("status"))["Service"]
            if stopped["Active"] or stopped["State"] != "inactive":
                raise AssertionError("Log capture ended without the dedicated service stopping")
            cli("service", "start")
            started = json.loads(cli("status"))["Service"]
            if not started["Active"] or started["State"] != "active":
                raise AssertionError("Dedicated log-test service did not restart")
            terminal.key(b"g")
            terminal.wait("Capturing live events")
            recovered_source = None
            for _ in range(5):
                recovered_source = log_event()
                terminal.read(0.15)
            terminal.wait("127.0.0.1:%s" % recovered_source)
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
            terminal.key(b"4")
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
            def cli(*arguments):
                result = subprocess.run(command + list(arguments), check=True,
                                        capture_output=True, timeout=20)
                return json.loads(result.stdout)

            def check_proxy_selection(expected):
                groups = cli("nodes")
                selected = next(group for group in groups if group["name"] == "PROXY")
                if selected["now"] != expected:
                    raise AssertionError("TUI selection was not confirmed by the real API")

            terminal.wait("Running")
            # Locate the sidebar entry so mouse coverage follows its spacing.
            terminal.wait("Settings")
            proxies_row = next(row for row, line in enumerate(terminal.screen.text().splitlines(), 1)
                               if re.match(r"^[ ›]*Proxies\b", line))
            terminal.key(f"\x1b[<0;5;{proxies_row}M\x1b[<0;5;{proxies_row}m".encode("ascii"))
            terminal.wait("Search groups or nodes")
            terminal.wait("PROXY")
            terminal.key(b"\x1b[C")  # sidebar -> PROXY; GLOBAL is reserved for Global mode
            terminal.key(b"\r")
            terminal.wait_selected_proxy("DIRECT")
            terminal.wait("REJECT")
            terminal.settle()
            for name in ("DIRECT", "REJECT"):
                unmeasured = re.compile(r"(?<!\S)" + re.escape(name) + r"[ \t]+—(?:[ \t]|$)")
                if not any(unmeasured.search(line) for line in terminal.screen.text().splitlines()):
                    raise AssertionError("Built-in proxy displayed a fabricated latency: " + name)
            terminal.key(b"\x1b[B")  # DIRECT
            terminal.key(b"\x1b[C")  # REJECT in the second column
            terminal.key(b"\r")
            terminal.wait("Node selected")
            terminal.wait_selected_proxy("REJECT")
            check_proxy_selection("REJECT")

            # Exercise a real candidate click as well as the sidebar click.
            terminal.click_text("DIRECT")
            terminal.wait_selected_proxy("DIRECT")
            check_proxy_selection("DIRECT")
            terminal.key(b"\x1b[C")
            terminal.key(b"\r")
            terminal.wait_selected_proxy("REJECT")
            check_proxy_selection("REJECT")

            # On this page r refreshes observations; it must not restart Mihomo.
            pid = cli("status")["Service"]["PID"]
            terminal.key(b"r")
            terminal.wait_absent("Refreshing")
            terminal.wait_selected_proxy("REJECT")
            if cli("status")["Service"]["PID"] != pid:
                raise AssertionError("Proxies refresh restarted the running core")
            terminal.key(b"3")
            terminal.wait("Built-in default")
            terminal.wait("configuration")
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
