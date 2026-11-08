#!/usr/bin/env python3
"""Verify the one-time sleepy logo entrance and click story in a real color PTY.

Uses disposable homes with startup guidance dismissed and random service names.
No core, service, or user configuration is installed or changed. Every invocation
creates a new output subdirectory so earlier verification evidence remains intact.
"""
import argparse
import codecs
import importlib.util
import json
import re
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
for key in ('NO_COLOR', 'COLORTERM', 'CLICOLOR', 'CLICOLOR_FORCE', 'TERM_PROGRAM', 'TMUX', 'TMUX_PANE'):
    os.environ.pop(key, None)
os.environ.update(LAZYCLASH_LANG='en', TERM='xterm-256color')
if sys.argv[3] == 'truecolor': os.environ['COLORTERM'] = 'truecolor'
if sys.argv[3] == 'no-color': os.environ['NO_COLOR'] = '1'
fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack('HHHH', int(sys.argv[2]), int(sys.argv[1]), 0, 0))
os.execv(sys.argv[4], sys.argv[4:])
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


def spawn(binary, size, home, profile):
    DEMO.prepare_home(home)
    columns, rows = size
    command = [sys.executable, "-c", WRAPPER, str(columns), str(rows), profile, str(binary),
               "--home", str(home), "--service-name", "lazyclash-logo-intro-" + uuid.uuid4().hex[:12]]
    terminal = SMOKE.Terminal(command)
    # The wrapper sets the actual window before exec, so startup is never
    # accidentally observed at Terminal's usual 120x40 default.
    terminal.screen = ColorScreen(columns, rows)
    terminal.logo_profile = profile
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
    if None in colors and terminal.logo_profile != "no-color":
        return None  # Startup capability negotiation or an incomplete repaint.
    return colors, center


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
        header_y = 1 if rows >= 24 else 0
        return (len(lines) == rows and lines[-1].find("q Quit") == columns - 8
                and lines[header_y][DEMO.SIDEBAR_WIDTH:].lstrip().startswith(page)
                and LOGO in lines[header_y])
    DEMO.wait_until(terminal, painted, "sidebar did not finish painting")


def click_story(terminal, size, output, baseline):
    origin = DEMO.locate_logo(terminal, size[1])
    x, y = origin
    terminal.send(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())
    started = time.monotonic()
    frames = []
    for index, (phase, center, bubble, offset) in enumerate(DEMO.FRAMES if y else DEMO.FRAMES[:5], 1):
        DEMO.wait_until(terminal, lambda: DEMO.logo_state(terminal, origin) == (center, bubble, offset),
                        "click story missed " + phase, timeout=2)
        (output / ("click-%02d.txt" % index)).write_text(terminal.screen.text() + "\n")
        colors = tuple(terminal.screen.colors[y][x + column] for column in (0, 1, 4, 5, 6, 7, 8, 9))
        warm = {"truecolor": (2, 249, 174, 88), "ansi256": (5, 215), "no-color": None}[terminal.logo_profile]
        expected = (warm,) * 8 if phase == "wake" else baseline
        if colors != expected:
            raise AssertionError("click story changed the warm wake or restored gradient: " + phase)
        frames.append({"phase": phase, "elapsed_ms": round((time.monotonic() - started) * 1000),
                       "foreground": colors})
    while time.monotonic() - started < 1.9:
        terminal.read(0.01)
    if DEMO.logo_state(terminal, origin) != ("💤", "", 0):
        raise AssertionError("click story did not settle")
    return frames


def color_rgb(color):
    if color is None:
        return 216, 222, 233
    if color[0] == 2:
        return color[1:]
    if color[0] == 5:
        index = color[1]
        if index >= 232:
            return (8 + (index - 232) * 10,) * 3
        if index >= 16:
            index -= 16
            cube = (0, 95, 135, 175, 215, 255)
            return cube[index // 36], cube[index // 6 % 6], cube[index % 6]
    raise AssertionError("unexpected logo color: " + repr(color))


def assert_gradient(colors, profile):
    if profile == "no-color":
        if any(color is not None for color in colors):
            raise AssertionError("NO_COLOR wordmark retained foreground colors")
        return
    mode = 2 if profile == "truecolor" else 5
    if any(color is None or color[0] != mode for color in colors):
        raise AssertionError("wordmark did not use its terminal color profile")
    if len(set(colors)) < 3:
        raise AssertionError("wordmark gradient collapsed to fewer than three colors")
    left, right = color_rgb(colors[0]), color_rgb(colors[-1])
    if not left[2] > left[0] or not right[0] > left[0]:
        raise AssertionError("wordmark did not move from blue toward purple")


def png_logo(screen, origin, path):
    """Redraw the actual PTY wordmark cells; this is not a native screenshot."""
    from PIL import Image, ImageDraw, ImageFont
    font = ImageFont.truetype("/System/Library/Fonts/Menlo.ttc", 28, index=1)
    emoji = ImageFont.truetype("/System/Library/Fonts/Apple Color Emoji.ttc", 32)
    cell_width = round(font.getlength("M"))
    cell_height = sum(font.getmetrics()) + 6
    image = Image.new("RGB", (16 * cell_width, 3 * cell_height), "#1d2227")
    draw = ImageDraw.Draw(image)
    x, y = origin
    for column in range(10):
        char = screen.cells[y][x + column]
        if not char:
            continue
        left = (column + 3) * cell_width
        top = cell_height
        if char == "💤":
            draw.text((left, top), char, font=emoji, embedded_color=True)
        else:
            draw.text((left, top), char, font=font, fill=color_rgb(screen.colors[y][x + column]))
    image.save(path)


def verify(binary, output, size, profile, png):
    output.mkdir()
    records = []
    frames = []
    with tempfile.TemporaryDirectory(prefix="lazyclash-logo-intro-") as temporary:
        terminal = spawn(binary, size, Path(temporary) / "home", profile)
        try:
            ready(terminal, size)
            baseline = observe(terminal, size, 1.5, "entrance", records)
            entrance = [record for record in records if record["phase"] == "entrance"]
            vectors = [tuple(tuple(color) if color is not None else None for color in record["foreground"])
                       for record in entrance]
            centers = []
            for record in entrance:
                if not centers or centers[-1]["center"] != record["center"]:
                    centers.append({"center": record["center"], "elapsed_ms": record["elapsed_ms"]})
            expected_centers = ["💤"] if profile == "no-color" else ["💤", "z ", " Z", "💤"]
            if [record["center"] for record in centers] != expected_centers:
                raise AssertionError("entrance center did not follow its profile's one-time sequence")
            assert_gradient(baseline, profile)
            if vectors[0] != baseline or vectors[-1] != baseline:
                raise AssertionError("entrance did not restore its original gradient")
            if profile == "no-color":
                if len(set(vectors)) != 1:
                    raise AssertionError("NO_COLOR played a brightness animation")
            else:
                if len(set(vectors)) < 3:
                    raise AssertionError("entrance did not brighten the gradient gradually")
                for column in range(8):
                    colors = [vector[column] for vector in vectors]
                    levels = [sum(weight * channel for weight, channel in zip((.2126, .7152, .0722), color_rgb(color)))
                              for color in colors]
                    peak = levels.index(max(levels))
                    if levels[peak] <= levels[0] + 1:
                        raise AssertionError("entrance never brightened logo column " + str(column))
                    if (any(a > b for a, b in zip(levels[:peak], levels[1:peak + 1]))
                            or any(a < b for a, b in zip(levels[peak:], levels[peak + 1:]))):
                        raise AssertionError("entrance reversed its gradual breath at column " + str(column))
                if profile == "truecolor":
                    if (2, 197, 239, 255) not in [vector[0] for vector in vectors] or (2, 234, 215, 255) not in [vector[-1] for vector in vectors]:
                        raise AssertionError("entrance did not reach both approved gradient highlights")
                first_change = entrance[1]["elapsed_ms"]
                settled = entrance[-1]["elapsed_ms"]
                if not 100 <= first_change <= 700 or not 600 <= settled <= 1400:
                    raise AssertionError("entrance timing did not match the startup delay and brief breath")
            if png:
                png_logo(terminal.screen, DEMO.locate_logo(terminal, size[1]), output / "logo-gradient.png")
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
            frames = click_story(terminal, size, output, baseline)
            observe(terminal, size, .25, "after-click", records, baseline)
            terminal.exit()
            return baseline
        finally:
            (output / "observations.json").write_text(json.dumps({"size": size, "profile": profile, "colors": records,
                                                                  "click_frames": frames}, indent=2) + "\n")
            (output / "terminal.ansi").write_bytes(terminal.raw)
            terminal.cleanup()
            sgr = re.findall(rb"\x1b\[([0-9;:]*)m", terminal.raw)
            if profile == "no-color" and any(value not in (b"", b"0") for value in sgr):
                raise AssertionError("NO_COLOR emitted styling")
            if profile == "ansi256" and re.search(rb"\x1b\[[0-9;:]*[34]8[;:]2[;:]", terminal.raw):
                raise AssertionError("ANSI256 emitted RGB styling")


def verify_compact_start(binary, output, baseline, profile):
    output.mkdir()
    records = []
    with tempfile.TemporaryDirectory(prefix="lazyclash-logo-intro-compact-") as temporary:
        terminal = spawn(binary, (80, 24), Path(temporary) / "home", profile)
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
    parser.add_argument("--profile", action="append", choices=("truecolor", "ansi256", "no-color"),
                        help="Repeat for a profile matrix; defaults to True Color")
    parser.add_argument("--png", action="store_true", help="Redraw actual PTY wordmark cells with Pillow/Menlo on macOS")
    args = parser.parse_args()
    binary = args.binary.resolve()
    if not binary.is_file():
        parser.error("--binary must point to an executable")
    output = args.output.resolve() / (time.strftime("run-%Y%m%d-%H%M%S-") + uuid.uuid4().hex[:6])
    output.mkdir(parents=True)
    for profile in args.profile or ["truecolor"]:
        profile_output = output / profile
        profile_output.mkdir()
        baseline = None
        for size in args.size or [(160, 45), (120, 24), (100, 20)]:
            baseline = verify(binary, profile_output / ("%dx%d" % size), size, profile, args.png)
            print("verified %s %dx%d gradient, entrance, no replay, and click story" % (profile, *size), flush=True)
        verify_compact_start(binary, profile_output / "80x24-start", baseline, profile)
        print("verified %s 80x24 startup skips entrance and expanding does not replay" % profile, flush=True)
    print("Evidence: " + str(output), flush=True)


if __name__ == "__main__":
    main()
