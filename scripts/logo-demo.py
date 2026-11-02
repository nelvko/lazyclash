#!/usr/bin/env python3
"""Capture and verify the sidebar logo's wake-and-dream animation in a real PTY.

The demo uses an isolated lazyclash home with startup guidance dismissed. It
does not install a core, create a service, change network settings, or read a
real user home. Pass --size repeatedly to verify multiple terminal sizes.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import uuid


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("lazyclash_pty_smoke", HERE / "pty-smoke.py")
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)

# Locale and terminal capability changes belong only to the disposable child.
WRAPPER = "import os, sys; os.environ['LAZYCLASH_LANG'] = 'en'; os.execv(sys.argv[1], sys.argv[1:])"
LOGO = "la💤yClash"
SIDEBAR_WIDTH = 20
# (phase, central two cells, character above the center, column offset).
FRAMES = [
    ("wake", "!!", "", 0),
    ("pause", "..", "", 0),
    ("doze", "z ", "", 0),
    ("sleep", " Z", "", 0),
    ("asleep", "💤", "", 0),
    ("dream-small", "💤", "z", 0),
    ("dream-rise", "💤", "Z", 1),
    ("dream-away", "💤", "Z", 2),
    ("dream-gap", "💤", "", 0),
    ("dream-small-again", "💤", "z", 0),
    ("dream-rise-again", "💤", "Z", 1),
    ("dream-gone", "💤", "", 0),
]


def prepare_home(home):
    """Keep logo-only captures independent of the startup release lookup."""
    home.mkdir()
    (home / "preferences.json").write_text(json.dumps({"GuideDismissed": True}) + "\n")


def wait_until(terminal, predicate, label, timeout=10):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if not terminal.screen.synchronized and terminal.screen.state == "text" and predicate():
            return
        terminal.read(min(0.02, max(0.0, deadline - time.monotonic())))
        if terminal.closed:
            break
    raise AssertionError(label + "\n" + terminal.screen.text())


def locate_logo(terminal, height):
    """Return cell coordinates rather than Python character indexes."""
    y = 1 if height >= 24 else 0
    row = terminal.screen.cells[y]
    for x in range(SIDEBAR_WIDTH - 10):
        if "".join(row[x:x + 10]) == LOGO:
            return x, y
    raise AssertionError("complete static logo is missing from sidebar header")


def logo_state(terminal, origin):
    x, y = origin
    row = terminal.screen.cells[y]
    if "".join(row[x:x + 2]) != "la" or "".join(row[x + 4:x + 10]) != "yClash":
        return None  # A partial repaint must never pass an animation assertion.
    if any(cell.strip() for cell in row[x + 10:SIDEBAR_WIDTH - 1]):
        raise AssertionError("logo grew a trailing suffix or shifted its wordmark")
    center = "".join(row[x + 2:x + 4])
    if not y:
        return center, "", 0
    top = terminal.screen.cells[y - 1]
    marks = [(column - x - 2, cell) for column, cell in enumerate(top[:SIDEBAR_WIDTH - 1]) if cell.strip()]
    if not marks:
        return center, "", 0
    if len(marks) != 1:
        return None
    offset, mark = marks[0]
    return center, mark, offset


def capture(binary, output, size, frames=None):
    columns, rows = size
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="lazyclash-logo-demo-") as temporary:
        home = Path(temporary) / "home"
        prepare_home(home)
        service = "lazyclash-logo-demo-" + uuid.uuid4().hex[:12]
        command = [sys.executable, "-c", WRAPPER, str(binary), "--home", str(home), "--service-name", service]
        terminal = SMOKE.Terminal(command)
        try:
            terminal.wait("Overview")
            terminal.resize(columns, rows)

            def ready():
                lines = terminal.screen.text().splitlines()
                # The new bottom-right footer proves SIGWINCH was painted.
                # Waiting only for Overview would accept the old 120x40 frame.
                return (len(lines) == rows and lines[-1].find("q Quit") == columns - 8
                        and any("Overview" in line[:SIDEBAR_WIDTH] for line in lines)
                        and LOGO in lines[1 if rows >= 24 else 0]
                        and "Loading state" not in terminal.screen.text()
                        and "Cancel in progress notice" not in terminal.screen.text())

            wait_until(terminal, ready, "startup or resize did not finish")
            origin = locate_logo(terminal, rows)
            if logo_state(terminal, origin) != ("💤", "", 0):
                raise AssertionError("initial logo is already animated")
            (output / "frame-00.txt").write_text(terminal.screen.text() + "\n")
            manifest = [{"file": "frame-00.txt", "phase": "idle", "elapsed_ms": 0}]
            x, y = origin
            # Send directly so the first 120ms frame is captured immediately.
            terminal.send(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())
            started = time.monotonic()
            expected_frames = FRAMES if y else FRAMES[:5]
            if frames is not None:
                expected_frames = expected_frames[:frames]
            for index, (phase, center, bubble, offset) in enumerate(expected_frames, 1):
                expected = center, bubble, offset
                wait_until(terminal, lambda: logo_state(terminal, origin) == expected,
                           "animation missed phase " + phase, timeout=2)
                filename = "frame-%02d.txt" % index
                (output / filename).write_text(terminal.screen.text() + "\n")
                manifest.append({"file": filename, "phase": phase,
                                 "elapsed_ms": round((time.monotonic() - started) * 1000)})
            if frames is None or frames >= len(FRAMES):
                # The last visible change precedes the final idle timer. Drain
                # the PTY through its end and ensure no bubble or suffix remains.
                while time.monotonic() - started < 1.9:
                    terminal.read(0.02)
                if logo_state(terminal, origin) != ("💤", "", 0):
                    raise AssertionError("logo did not settle after its animation")
                filename = "frame-%02d.txt" % len(manifest)
                (output / filename).write_text(terminal.screen.text() + "\n")
                manifest.append({"file": filename, "phase": "settled",
                                 "elapsed_ms": round((time.monotonic() - started) * 1000)})
            (output / "frames.json").write_text(json.dumps({"columns": columns, "rows": rows,
                                                           "frames": manifest}, indent=2) + "\n")
            terminal.exit()
            return len(manifest)
        finally:
            terminal.cleanup()


def size_arg(value):
    try:
        columns, rows = (int(part) for part in value.lower().split("x"))
    except ValueError as exc:
        raise argparse.ArgumentTypeError("use columnsxrows, such as 160x45") from exc
    if columns < 100 or rows < 20:
        raise argparse.ArgumentTypeError("the persistent sidebar requires at least 100x20")
    return columns, rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--size", type=size_arg, action="append", help="terminal size; repeat for a matrix (default: 160x45)")
    parser.add_argument("--frames", type=int, help="capture only this many animation phases; omit for the full sequence")
    parser.add_argument("--interval", type=float, help=argparse.SUPPRESS)  # Accepted for older demo commands; capture follows real frames.
    args = parser.parse_args()
    if not args.binary.is_file() or not os.access(args.binary, os.X_OK):
        parser.error("--binary must point to an executable")
    if args.frames is not None and args.frames < 1:
        parser.error("--frames must be positive")
    sizes = args.size or [(160, 45)]
    for size in sizes:
        output = args.output.resolve()
        if len(sizes) > 1:
            output /= "%dx%d" % size
        count = capture(args.binary.resolve(), output, size, args.frames)
        print("verified %dx%d; wrote %d frames to %s" % (*size, count, output))


if __name__ == "__main__":
    main()
