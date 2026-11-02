#!/usr/bin/env python3
"""Verify the one-time sleepy logo entrance and click story in a real color PTY.

Uses disposable empty homes and random service names. No core, service, or user
configuration is installed or changed. Every invocation creates a new output
subdirectory so earlier verification evidence remains intact.
"""
import argparse
import codecs
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import time
import unicodedata
import uuid


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("lazyclash_logo_demo", HERE / "logo-demo.py")
DEMO = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DEMO)
SMOKE = DEMO.SMOKE
LOGO = DEMO.LOGO
WRAPPER = """import fcntl, os, struct, sys, termios
os.environ.pop('NO_COLOR', None)
os.environ.update(LAZYCLASH_LANG='en', TERM='xterm-256color', COLORTERM='truecolor')
fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack('HHHH', int(sys.argv[2]), int(sys.argv[1]), 0, 0))
os.execv(sys.argv[3], sys.argv[3:])
"""


class ColorScreen(SMOKE.Screen):
    """Retain foreground SGR only; reuse the existing VT geometry parser."""
    def __init__(self, columns, rows):
        super().__init__(columns, rows)
        self.foreground = None
        self.colors = [[None] * columns for _ in range(rows)]
        self.color_decoder = codecs.getincrementaldecoder("utf-8")("replace")

    def resize(self, columns, rows):
        self.colors = [(line[:columns] + [None] * columns)[:columns] for line in self.colors[:rows]]
        self.colors.extend([[None] * columns for _ in range(rows - len(self.colors))])
        super().resize(columns, rows)

    def scroll(self, top, bottom, count):
        count = max(-(bottom - top + 1), min(count, bottom - top + 1))
        region = self.colors[top:bottom + 1]
        blank = [[None] * self.columns for _ in range(abs(count))]
        self.colors[top:bottom + 1] = (region[count:] + blank) if count >= 0 else (blank + region[:count])
        super().scroll(top, bottom, count)

    def feed(self, chunk):
        for char in self.color_decoder.decode(chunk):
            printable = (self.state == "text" and ord(char) >= 32 and char != "\x7f"
                         and not unicodedata.combining(char) and char not in "\ufe0f\u200d")
            super().feed(char.encode("utf-8"))
            if printable:
                width = 2 if unicodedata.east_asian_width(char) in ("W", "F") else 1
                self.colors[self.y][self.x - width:self.x] = [self.foreground] * width

    def csi(self, final, sequence):
        if final == "m":
            # Bubble Tea emits semicolon SGR; accept colon RGB forms as well.
            values = [int(value) if value.isdigit() else 0 for value in sequence.replace(":", ";").split(";")]
            index = 0
            while index < len(values):
                code = values[index]
                if code in (0, 39):
                    self.foreground = None
                elif code in (38, 48, 58) and index + 1 < len(values):
                    mode = values[index + 1]
                    length = 3 if mode == 2 else (1 if mode == 5 else 0)
                    start = index + 2
                    # ISO colon notation may include an empty colorspace slot.
                    if mode == 2 and ":" in sequence and index + 5 < len(values):
                        start += 1
                    if length and start + length <= len(values):
                        if code == 38:
                            self.foreground = tuple([mode] + values[start:start + length])
                        index = start + length - 1
                elif 30 <= code <= 37 or 90 <= code <= 97:
                    self.foreground = (code,)
                index += 1
            return
        if sequence.startswith("?") and final == "h" and sequence in ("?1049", "?1047"):
            self.colors = [[None] * self.columns for _ in range(self.rows)]
        values = sequence.split(";")
        numbers = [int(value) if value.isdigit() else 0 for value in values]
        n = numbers[0] or 1
        if final == "J":
            mode = numbers[0]
            if mode in (2, 3):
                self.colors = [[None] * self.columns for _ in range(self.rows)]
            elif mode == 0:
                self.colors[self.y][self.x:] = [None] * (self.columns - self.x)
                for row in range(self.y + 1, self.rows):
                    self.colors[row] = [None] * self.columns
            elif mode == 1:
                for row in range(self.y):
                    self.colors[row] = [None] * self.columns
                self.colors[self.y][:self.x + 1] = [None] * (self.x + 1)
        elif final == "K":
            mode = numbers[0]
            start, end = (0, self.columns) if mode == 2 else ((0, self.x + 1) if mode == 1 else (self.x, self.columns))
            self.colors[self.y][start:end] = [None] * (end - start)
        elif final == "X":
            end = min(self.columns, self.x + n)
            self.colors[self.y][self.x:end] = [None] * (end - self.x)
        elif final == "P":
            self.colors[self.y] = (self.colors[self.y][:self.x] + self.colors[self.y][self.x + n:] + [None] * n)[:self.columns]
        elif final == "@":
            self.colors[self.y] = (self.colors[self.y][:self.x] + [None] * n + self.colors[self.y][self.x:])[:self.columns]
        super().csi(final, sequence)


def spawn(binary, size, home):
    columns, rows = size
    command = [sys.executable, "-c", WRAPPER, str(columns), str(rows), str(binary),
               "--home", str(home), "--service-name", "lazyclash-logo-intro-" + uuid.uuid4().hex[:12]]
    terminal = SMOKE.Terminal(command)
    # The wrapper sets the actual window before exec, so startup is never
    # accidentally observed at Terminal's usual 120x40 default.
    terminal.screen = ColorScreen(columns, rows)
    return terminal


def snapshot(terminal, origin, sidebar=True, entrance=False):
    if terminal.screen.synchronized or terminal.screen.state != "text":
        return None
    x, y = origin
    if sidebar:
        state = DEMO.logo_state(terminal, origin)
        if state is None:
            return None
        center, bubble, offset = state
        allowed = ("💤", "z ", " Z") if entrance else ("💤",)
        if center not in allowed or bubble or offset:
            raise AssertionError("entrance or idle replay changed the fixed wordmark or added a dream bubble")
    elif "".join(terminal.screen.cells[y][x:x + 10]) != LOGO:
        raise AssertionError("compact title changed its logo characters")
    else:
        center = "💤"
    # Emoji glyph colors belong to the terminal; inspect the eight ASCII cells.
    colors = tuple(terminal.screen.colors[y][x + offset] for offset in (0, 1, 4, 5, 6, 7, 8, 9))
    if None in colors or len(set(colors)) != 1:
        return None  # Startup capability negotiation or an incomplete repaint.
    return colors[0], center


def observe(terminal, size, duration, phase, records, baseline=None, origin=None, sidebar=True):
    origin = origin or DEMO.locate_logo(terminal, size[1])
    started = time.monotonic()
    previous = None
    while time.monotonic() - started < duration:
        sample = snapshot(terminal, origin, sidebar, phase == "entrance")
        if sample is not None:
            color, center = sample
            if baseline is not None and color != baseline:
                raise AssertionError(phase + " replayed the entrance color animation")
            if sample != previous:
                records.append({"phase": phase, "elapsed_ms": round((time.monotonic() - started) * 1000),
                                "foreground": color, "center": center})
                previous = sample
        terminal.read(0.01)
        if terminal.closed:
            raise AssertionError("TUI exited during " + phase)
    if previous is None:
        raise AssertionError("no complete colored logo was rendered during " + phase)
    return previous[0]


def ready(terminal, size, page="Overview"):
    columns, rows = size
    def painted():
        lines = terminal.screen.text().splitlines()
        return (len(lines) == rows and lines[-1].find("q Quit") == columns - 8
                and page + " •" in terminal.screen.text()
                and LOGO in lines[1 if rows >= 24 else 0])
    DEMO.wait_until(terminal, painted, "sidebar did not finish painting")


def click_story(terminal, size, output):
    origin = DEMO.locate_logo(terminal, size[1])
    x, y = origin
    terminal.send(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())
    started = time.monotonic()
    frames = []
    for index, (phase, center, bubble, offset) in enumerate(DEMO.FRAMES if y else DEMO.FRAMES[:5], 1):
        DEMO.wait_until(terminal, lambda: DEMO.logo_state(terminal, origin) == (center, bubble, offset),
                        "click story missed " + phase, timeout=2)
        (output / ("click-%02d.txt" % index)).write_text(terminal.screen.text() + "\n")
        frames.append({"phase": phase, "elapsed_ms": round((time.monotonic() - started) * 1000)})
    while time.monotonic() - started < 1.9:
        terminal.read(0.01)
    if DEMO.logo_state(terminal, origin) != ("💤", "", 0):
        raise AssertionError("click story did not settle")
    return frames


def verify(binary, output, size):
    output.mkdir()
    records = []
    frames = []
    with tempfile.TemporaryDirectory(prefix="lazyclash-logo-intro-") as temporary:
        terminal = spawn(binary, size, Path(temporary) / "home")
        try:
            ready(terminal, size)
            baseline = observe(terminal, size, 1.5, "entrance", records)
            entrance = [record for record in records if record["phase"] == "entrance"]
            colors = [tuple(record["foreground"]) for record in entrance]
            centers = []
            for record in entrance:
                if not centers or centers[-1]["center"] != record["center"]:
                    centers.append({"center": record["center"], "elapsed_ms": record["elapsed_ms"]})
            if [record["center"] for record in centers] != ["💤", "z ", " Z", "💤"]:
                raise AssertionError("entrance center did not follow sleep, small z, big Z, sleep")
            if len(set(colors)) < 3 or colors[0] != baseline:
                raise AssertionError("entrance must brighten gradually and return to its initial cyan")
            if any(color[0] != 2 or len(color) != 4 for color in colors):
                raise AssertionError("True Color logo foreground was not emitted")
            brightness = lambda color: sum(weight * channel for weight, channel in zip((.2126, .7152, .0722), color[1:]))
            levels = list(map(brightness, colors))
            peak = levels.index(max(levels))
            if baseline != (2, 133, 179, 224) or colors[peak] != (2, 197, 239, 255):
                raise AssertionError("entrance did not use the approved cyan and sleepy highlight")
            if levels[peak] <= brightness(baseline) + 1:
                raise AssertionError("entrance never became brighter than its resting cyan")
            if (any(a > b for a, b in zip(levels[:peak], levels[1:peak + 1]))
                    or any(a < b for a, b in zip(levels[peak:], levels[peak + 1:]))):
                raise AssertionError("entrance must contain exactly one gradual breath")
            first_change = entrance[1]["elapsed_ms"]
            settled = entrance[-1]["elapsed_ms"]
            if not 100 <= first_change <= 700 or not 600 <= settled <= 1400:
                raise AssertionError("entrance timing did not match the startup delay and brief breath")
            (output / "entrance-settled.txt").write_text(terminal.screen.text() + "\n")
            for key, page in ((b"2", "Proxies"), (b"1", "Overview")):
                terminal.send(key)
                ready(terminal, size, page)
                observe(terminal, size, 1.2, "navigate-" + page.lower(), records, baseline)
            other = (size[0], 24 if size[1] == 20 else 20)
            for resized in (other, size):
                terminal.resize(*resized)
                ready(terminal, resized)
                observe(terminal, resized, 1.2, "resize-%dx%d" % resized, records, baseline)
            terminal.send(b"?")
            terminal.wait("Help / Overview")
            terminal.send(b"\x1b")
            terminal.wait_absent("Help / Overview")
            ready(terminal, size)
            observe(terminal, size, 1.2, "close-help", records, baseline)
            frames = click_story(terminal, size, output)
            observe(terminal, size, .25, "after-click", records, baseline)
            terminal.exit()
            return baseline
        finally:
            (output / "observations.json").write_text(json.dumps({"size": size, "colors": records,
                                                                  "click_frames": frames}, indent=2) + "\n")
            (output / "terminal.ansi").write_bytes(terminal.raw)
            terminal.cleanup()


def verify_compact_start(binary, output, baseline):
    output.mkdir()
    records = []
    with tempfile.TemporaryDirectory(prefix="lazyclash-logo-intro-compact-") as temporary:
        terminal = spawn(binary, (80, 24), Path(temporary) / "home")
        try:
            terminal.wait("Overview")
            # Compact layouts retain a static wordmark in the page title.
            # Its suffix is " / Overview", so sidebar-only tail checks do not apply.
            origin = next((x, y) for y, row in enumerate(terminal.screen.cells[:2])
                          for x in range(terminal.screen.columns - 10)
                          if "".join(row[x:x + 10]) == LOGO)
            observe(terminal, (80, 24), 1.3, "compact-start", records, baseline, origin, False)
            terminal.resize(160, 45)
            ready(terminal, (160, 45))
            observe(terminal, (160, 45), 1.4, "compact-start-then-expand", records, baseline)
            (output / "expanded.txt").write_text(terminal.screen.text() + "\n")
            terminal.exit()
        finally:
            (output / "observations.json").write_text(json.dumps(records, indent=2) + "\n")
            (output / "terminal.ansi").write_bytes(terminal.raw)
            terminal.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--size", type=DEMO.size_arg, action="append")
    args = parser.parse_args()
    binary = args.binary.resolve()
    if not binary.is_file():
        parser.error("--binary must point to an executable")
    output = args.output.resolve() / (time.strftime("run-%Y%m%d-%H%M%S-") + uuid.uuid4().hex[:6])
    output.mkdir(parents=True)
    baseline = None
    for size in args.size or [(160, 45), (120, 24), (100, 20)]:
        baseline = verify(binary, output / ("%dx%d" % size), size)
        print("verified %dx%d entrance, no replay, and click story" % size, flush=True)
    verify_compact_start(binary, output / "80x24-start", baseline)
    print("verified 80x24 startup skips entrance and expanding does not replay", flush=True)
    print("Evidence: " + str(output), flush=True)


if __name__ == "__main__":
    main()
