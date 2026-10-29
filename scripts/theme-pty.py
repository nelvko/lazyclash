#!/usr/bin/env python3
"""Verify Mariana rendering through an isolated, real controlling PTY.

Requires an already built --binary (absolute path). It does not build, create a
VM, install a core/service, query releases or change system network settings.
--output stores per-profile raw PTY captures and current-screen text snapshots.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import uuid


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("lazyclash_pty_smoke", HERE / "pty-smoke.py")
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)

# Terminal deliberately defaults to NO_COLOR for the lifecycle test suite. This
# child-only wrapper selects each color capability without changing that suite,
# the caller's environment, or shell startup files.
WRAPPER = """
import os, sys
profile = sys.argv[1]
for name in ('NO_COLOR', 'COLORTERM', 'CLICOLOR', 'CLICOLOR_FORCE', 'TERM_PROGRAM', 'TMUX', 'TMUX_PANE'):
    os.environ.pop(name, None)
os.environ['TERM'] = 'xterm-256color'
if profile == 'truecolor':
    os.environ['COLORTERM'] = 'truecolor'
elif profile == 'no-color':
    os.environ['NO_COLOR'] = '1'
os.execv(sys.argv[2], sys.argv[2:])
"""
SIZES = [(160, 45), (120, 32), (80, 24), (60, 20), (120, 18)]
PREFIX = "https://pty.example.invalid/"
SGR = re.compile(rb"\x1b\[([0-9;:]*)m")
RGB = re.compile(rb"\x1b\[[0-9;:]*[34]8[;:]2[;:]")
INDEXED = re.compile(rb"\x1b\[[0-9;:]*[34]8[;:]5[;:]")
OSC_SET = re.compile(rb"\x1b\](?:10|11);(?!\?)")


def save_screen(terminal, output, profile, name):
    (output / (profile + "-" + name + ".txt")).write_text(terminal.screen.text() + "\n")


def enter_network(terminal):
    terminal.key(b"\r")
    terminal.wait("Mihomo · Source + Connection")


def masked_input(terminal):
    terminal.wait("Prefix URL")
    # The current-screen emulator handles the v2 renderer's cell-diff output.
    # Reading raw historical text here could falsely retain an old focus/input.
    lines = terminal.screen.text().splitlines()
    return any("›" in line and "*" * len(PREFIX) in line for line in lines)


def exercise(binary, output, profile):
    terminal = None
    with tempfile.TemporaryDirectory(prefix="lazyclash-theme-pty-") as temporary:
        home = Path(temporary) / "home"
        service = "lazyclash-theme-" + uuid.uuid4().hex[:12]
        command = [sys.executable, "-c", WRAPPER, profile, str(binary),
                   "--home", str(home), "--service-name", service]
        try:
            terminal = SMOKE.Terminal(command)
            terminal.wait("State refreshed")
            mark = len(terminal.text())
            terminal.resize(160, 45)
            terminal.wait("Kernel and environment", after=mark)
            terminal.wait("Get started")
            save_screen(terminal, output, profile, "overview")
            terminal.key(b"i")
            terminal.wait("Mihomo · Core")
            terminal.wait("Choose a proxy core")
            save_screen(terminal, output, profile, "core")

            # Continue -> Core -> Network settings, without selecting Continue
            # or launching any version query. Tab focus is exercised in the PTY.
            terminal.key(b"\t")
            terminal.key(b"\t")
            enter_network(terminal)
            terminal.key(b"\r")  # Official -> URL prefix accelerator
            terminal.wait("Prefix URL")
            terminal.key(b"\t")
            terminal.key(PREFIX.encode("ascii"))
            terminal.wait("*" * len(PREFIX))
            for columns, rows in SIZES:
                mark = len(terminal.text())
                terminal.resize(columns, rows)
                terminal.wait("Prefix URL", after=mark)
                terminal.wait("*" * len(PREFIX))
                if not masked_input(terminal):
                    raise AssertionError("resize lost the draft input or its focus at %sx%s" % (columns, rows))
                terminal.wait("Esc")
                save_screen(terminal, output, profile, "network-%sx%s" % (columns, rows))
            mark = len(terminal.text())
            terminal.resize(120, 32)
            terminal.wait("*" * len(PREFIX), after=mark)
            terminal.key(b"\x1b")
            terminal.wait_absent("Mihomo · Source + Connection")
            terminal.wait("Mihomo · Core")
            enter_network(terminal)  # restored Network settings trigger focus
            terminal.wait("Official / GitHub direct")
            terminal.wait_absent("Prefix URL")
            terminal.key(b"\x1b")
            terminal.wait_absent("Mihomo · Source + Connection")
            terminal.key(b"\x1b")
            terminal.wait_absent("Install proxy core")
            terminal.wait("Get started")
            terminal.exit()

            raw = bytes(terminal.raw)
            if PREFIX.encode("ascii") in raw:
                raise AssertionError("hidden input appeared in terminal output")
            if OSC_SET.search(raw):
                raise AssertionError("TUI set the terminal's global foreground/background")
            if profile == "truecolor" and not RGB.search(raw):
                raise AssertionError("True Color run emitted no RGB styling")
            if profile == "256" and (RGB.search(raw) or not INDEXED.search(raw)):
                raise AssertionError("256-color run did not exclusively use its supported color representation")
            if profile == "no-color":
                # Terminal renderers may reset SGR on entry/exit; NO_COLOR must
                # contain no actual foreground/background/decorative styles.
                styled = [m.group(1) for m in SGR.finditer(raw) if m.group(1) not in (b"", b"0")]
                if styled:
                    raise AssertionError("NO_COLOR run emitted SGR styling: " + repr(styled[:4]))
            if (home / "installation").exists() or (home / "preferences.json").exists():
                raise AssertionError("read-only theme exercise installed a core or saved preferences")
            return {"profile": profile, "status": "passed", "sizes": SIZES,
                    "rgb": bool(RGB.search(raw)), "indexed": bool(INDEXED.search(raw))}
        finally:
            if terminal is not None:
                (output / (profile + ".ansi")).write_bytes(terminal.raw)
                terminal.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True, help="Absolute path to an already built lazyclash")
    parser.add_argument("--output", type=Path, required=True, help="Directory for isolated PTY evidence")
    parser.add_argument("--profile", choices=("all", "truecolor", "256", "no-color"), default="all")
    args = parser.parse_args()
    if not args.binary.is_absolute() or not args.binary.is_file() or not os.access(args.binary, os.X_OK):
        parser.error("--binary must be an absolute path to an executable file")
    args.output.mkdir(parents=True, exist_ok=True)
    profiles = ("truecolor", "256", "no-color") if args.profile == "all" else (args.profile,)
    results = [exercise(args.binary, args.output, profile) for profile in profiles]
    (args.output / "results.json").write_text(json.dumps(results, indent=2) + "\n")
    for result in results:
        print("Mariana PTY %s: passed (%s sizes)" % (result["profile"], len(result["sizes"])))


if __name__ == "__main__":
    main()
