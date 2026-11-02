#!/usr/bin/env python3
"""Verify first-run installation guidance through a real controlling PTY.

Uses disposable empty homes and random service names. The production binary
shows inline guidance and its read-only version check, without installing
a core or starting a service. --test-binary selects an explicit in-memory Go
fixture that also exercises one-click installation and cancellation, with no
network, executable or service. Captures current VT screens and raw ANSI.
Optional --png redraws the actual PTY cell state with Pillow; these are not
native terminal-app screenshots.
"""
import argparse
import codecs
import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import time
import unicodedata
import uuid


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("lazyclash_onboarding_terminal", HERE / "pty-smoke.py")
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)
PREVIEW_SPEC = importlib.util.spec_from_file_location("lazyclash_onboarding_preview", HERE / "theme-preview.py")
PREVIEW = importlib.util.module_from_spec(PREVIEW_SPEC)
PREVIEW_SPEC.loader.exec_module(PREVIEW)
SIZES = [(160, 45), (120, 32), (80, 24), (60, 20), (120, 18)]
WRAPPER = """import fcntl, os, struct, sys, termios
for key in ('NO_COLOR', 'COLORTERM', 'CLICOLOR', 'CLICOLOR_FORCE', 'TERM_PROGRAM', 'TMUX', 'TMUX_PANE'):
    os.environ.pop(key, None)
os.environ.update(TERM='xterm-256color', LAZYCLASH_LANG=sys.argv[4])
if sys.argv[3] == 'truecolor': os.environ['COLORTERM'] = 'truecolor'
if sys.argv[3] == 'no-color': os.environ['NO_COLOR'] = '1'
fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack('HHHH', int(sys.argv[2]), int(sys.argv[1]), 0, 0))
os.execv(sys.argv[5], sys.argv[5:])
"""
SGR = re.compile(rb"\x1b\[([0-9;:]*)m")
RGB = re.compile(rb"\x1b\[[0-9;:]*[34]8[;:]2[;:]")
INDEXED = re.compile(rb"\x1b\[[0-9;:]*[34]8[;:]5[;:]")
OSC_SET = re.compile(rb"\x1b\](?:10|11);(?!\?)")
DEFAULT_STYLE = ("#d8dee9", "#303841", False, False)


class StyledScreen(SMOKE.Screen):
    """Keep SGR attributes alongside the existing, tested VT cell geometry."""
    def __init__(self, columns, rows):
        super().__init__(columns, rows)
        self.style = DEFAULT_STYLE
        self.styles = [[self.style] * columns for _ in range(rows)]
        self.style_decoder = codecs.getincrementaldecoder("utf-8")("replace")

    def resize(self, columns, rows):
        self.styles = [(row[:columns] + [self.style] * columns)[:columns] for row in self.styles[:rows]]
        self.styles.extend([[self.style] * columns for _ in range(rows - len(self.styles))])
        super().resize(columns, rows)

    def scroll(self, top, bottom, count):
        count = max(-(bottom - top + 1), min(count, bottom - top + 1))
        region = self.styles[top:bottom + 1]
        blank = [[self.style] * self.columns for _ in range(abs(count))]
        self.styles[top:bottom + 1] = region[count:] + blank if count >= 0 else blank + region[:count]
        super().scroll(top, bottom, count)

    def feed(self, chunk):
        for char in self.style_decoder.decode(chunk):
            printable = (self.state == "text" and ord(char) >= 32 and char != "\x7f"
                         and not unicodedata.combining(char) and char not in "\ufe0f\u200d")
            super().feed(char.encode("utf-8"))
            if printable:
                width = 2 if unicodedata.east_asian_width(char) in "WF" else 1
                self.styles[self.y][self.x - width:self.x] = [self.style] * width

    def csi(self, final, sequence):
        if final == "m":
            values = [int(value) if value.isdigit() else 0 for value in sequence.replace(":", ";").split(";")]
            fg, bg, bold, underline = self.style
            index = 0
            while index < len(values):
                code = values[index]
                if code == 0:
                    fg, bg, bold, underline = DEFAULT_STYLE
                elif code in (1, 22):
                    bold = code == 1
                elif code in (4, 24):
                    underline = code == 4
                elif code == 39:
                    fg = DEFAULT_STYLE[0]
                elif code == 49:
                    bg = DEFAULT_STYLE[1]
                elif code in (38, 48, 58) and index + 1 < len(values):
                    mode = values[index + 1]
                    length = 3 if mode == 2 else (1 if mode == 5 else 0)
                    start = index + 2
                    if mode == 2 and ":" in sequence and index + 5 < len(values):
                        start += 1
                    if length and start + length <= len(values):
                        color = tuple(values[start:start + length]) if mode == 2 else PREVIEW.palette(values[start])
                        if code == 38:
                            fg = color
                        elif code == 48:
                            bg = color
                        index = start + length - 1
                elif 30 <= code <= 37 or 90 <= code <= 97:
                    fg = PREVIEW.palette(code - 30 if code < 90 else code - 90 + 8)
                elif 40 <= code <= 47 or 100 <= code <= 107:
                    bg = PREVIEW.palette(code - 40 if code < 100 else code - 100 + 8)
                index += 1
            self.style = fg, bg, bold, underline
            return
        if sequence.startswith("?") and final == "h" and sequence in ("?1049", "?1047"):
            self.styles = [[self.style] * self.columns for _ in range(self.rows)]
        numbers = [int(value) if value.isdigit() else 0 for value in sequence.split(";")]
        n = numbers[0] or 1
        if final == "J":
            mode = numbers[0]
            if mode in (2, 3):
                self.styles = [[self.style] * self.columns for _ in range(self.rows)]
            elif mode == 0:
                self.styles[self.y][self.x:] = [self.style] * (self.columns - self.x)
                for row in range(self.y + 1, self.rows):
                    self.styles[row] = [self.style] * self.columns
            elif mode == 1:
                for row in range(self.y):
                    self.styles[row] = [self.style] * self.columns
                self.styles[self.y][:self.x + 1] = [self.style] * (self.x + 1)
        elif final == "K":
            start, end = (0, self.columns) if numbers[0] == 2 else ((0, self.x + 1) if numbers[0] == 1 else (self.x, self.columns))
            self.styles[self.y][start:end] = [self.style] * (end - start)
        elif final == "X":
            end = min(self.columns, self.x + n)
            self.styles[self.y][self.x:end] = [self.style] * (end - self.x)
        elif final == "P":
            self.styles[self.y] = (self.styles[self.y][:self.x] + self.styles[self.y][self.x + n:] + [self.style] * n)[:self.columns]
        elif final == "@":
            self.styles[self.y] = (self.styles[self.y][:self.x] + [self.style] * n + self.styles[self.y][self.x:])[:self.columns]
        super().csi(final, sequence)


def settle(terminal):
    deadline, quiet = time.monotonic() + 4, 0
    while time.monotonic() < deadline:
        mark = len(terminal.raw)
        terminal.read(0.03)
        complete = not terminal.screen.synchronized and terminal.screen.state == "text"
        quiet = quiet + 1 if len(terminal.raw) == mark and complete else 0
        if quiet == 2:
            return
    raise AssertionError("PTY did not settle\n" + terminal.screen.text())


def png(screen, path, font_path, cjk_path, size):
    from PIL import Image, ImageDraw, ImageFont
    font = ImageFont.truetype(str(font_path), size)
    cjk = ImageFont.truetype(str(cjk_path), size) if cjk_path else font
    if cjk_path and cjk_path.suffix.lower() == ".ttc":
        # macOS PingFang's default face is HK and contains blank glyphs for
        # some simplified characters. Prefer the SC regular face for zh-Hans.
        for index in range(32):
            try:
                candidate = ImageFont.truetype(str(cjk_path), size, index=index)
            except OSError:
                break
            family, weight = candidate.getname()
            if "SC" in family and weight == "Regular":
                cjk = candidate
                break
    emoji_path = Path("/System/Library/Fonts/Apple Color Emoji.ttc")
    emoji = ImageFont.truetype(str(emoji_path), 20) if emoji_path.is_file() else None
    bold = font
    if font_path.suffix.lower() == ".ttc":
        for index in range(1, 8):
            try:
                candidate = ImageFont.truetype(str(font_path), size, index=index)
            except OSError:
                break
            if candidate.getname()[1] == "Bold":
                bold = candidate
                break
    cell_w = round(font.getlength("M"))
    ascent, descent = font.getmetrics()
    cell_h = ascent + descent + 3
    image = Image.new("RGB", (screen.columns * cell_w, screen.rows * cell_h), DEFAULT_STYLE[1])
    draw = ImageDraw.Draw(image)
    for y, row in enumerate(screen.cells):
        for x, text in enumerate(row):
            if not text:
                continue
            fg, bg, heavy, underline = screen.styles[y][x]
            cells = 2 if unicodedata.east_asian_width(text[0]) in "WF" else 1
            left, top = x * cell_w, y * cell_h
            draw.rectangle((left, top, left + cells * cell_w - 1, top + cell_h - 1), fill=bg)
            chosen = cjk if ord(text[0]) >= 0x2E80 else (bold if heavy else font)
            if 0x2800 <= ord(text[0]) <= 0x28FF:
                dots = ord(text[0]) - 0x2800
                radius = max(1, cell_w // 10)
                for bit, (column, row) in enumerate(((0, 0), (0, 1), (0, 2), (1, 0), (1, 1), (1, 2), (0, 3), (1, 3))):
                    if dots & (1 << bit):
                        center_x = left + round(cell_w * (column + 1) / 3)
                        center_y = top + round(cell_h * (row + 1) / 5)
                        draw.ellipse((center_x - radius, center_y - radius, center_x + radius, center_y + radius), fill=fg)
            elif text in "│─┌┐└┘├┤┬┴┼":
                center_x, center_y = left + cell_w // 2, top + cell_h // 2
                if text in "│┌┐├┤┬┼":
                    draw.line((center_x, center_y, center_x, top + cell_h - 1), fill=fg)
                if text in "│└┘├┤┴┼":
                    draw.line((center_x, top, center_x, center_y), fill=fg)
                if text in "─┐┘┤┬┴┼":
                    draw.line((left, center_y, center_x, center_y), fill=fg)
                if text in "─┌└├┬┴┼":
                    draw.line((center_x, center_y, left + cell_w - 1, center_y), fill=fg)
            elif text == "💤" and emoji:
                draw.text((left + max(0, (cells * cell_w - 20) // 2), top + max(0, (cell_h - 20) // 2)),
                          text, font=emoji, embedded_color=True)
            elif text == "▔":
                draw.rectangle((left, top, left + cell_w - 1, top + max(1, round(cell_h / 8)) - 1), fill=fg)
            elif text == "▌":
                draw.rectangle((left, top, left + max(1, round(cell_w / 2)) - 1, top + cell_h - 1), fill=fg)
            else:
                draw.text((left, top + 1), text, font=chosen, fill=fg)
            if underline and text != " ":
                draw.line((left, top + ascent + 2, left + cells * cell_w - 1, top + ascent + 2), fill=fg)
    image.save(path)


def capture(terminal, output, name, args):
    # Gaps between complete frames are shorter than the busy spinner period.
    # Drain all partial PTY chunks before rendering a screenshot, including on
    # terminals where synchronized output has not been negotiated.
    settle(terminal)
    (output / (name + ".txt")).write_text(terminal.screen.text() + "\n")
    (output / (name + ".ansi")).write_bytes(bytes(terminal.raw))
    if args.png:
        png(terminal.screen, output / (name + ".png"), args.font, args.cjk_font, args.font_size)


def assert_uninstalled(home):
    if (home / "installation").exists():
        raise AssertionError("PTY exercise installed a core")
    preferences = home / "preferences.json"
    if preferences.exists() and json.loads(preferences.read_text()).get("GuideDismissed", False):
        raise AssertionError("Set up later permanently dismissed the guide")


def focus_control(terminal, label):
    """Use the real focus order; focused controls may scroll into view."""
    for _ in range(24):
        settle(terminal)
        if re.search(r"›\s*(?:\[\s*)?" + re.escape(label), terminal.screen.text()):
            return
        terminal.key(b"\t")
    raise AssertionError("Control is not keyboard accessible: " + label + "\n" + terminal.screen.text())


def words(language):
    return ({"overview": "概览", "title": "安装 Mihomo 内核", "later": "稍后设置",
             "install": "下载并安装", "downloading": "正在下载", "cancelled": "安装已取消，清理已完成",
             "routing": "路由模式", "profiles": "配置", "quit": "q 退出", "more": "更多选项",
             "method": "下载方式", "network": "下载方式", "prefix": "加速地址",
             "choices": ("直连", "使用环境代理", "GitHub 加速"), "retry": "重试", "back": "返回概览",
             "start": "启动内核", "ready": "默认配置已就绪", "apply": "应用"}
            if language == "zh-Hans" else
            {"overview": "Overview", "title": "Install the Mihomo core", "later": "Set up later",
             "install": "Download and install", "downloading": "Downloading", "cancelled": "Installation canceled; cleanup completed",
             "routing": "Routing mode", "profiles": "Profiles", "quit": "q Quit", "more": "More options",
             "method": "Download method", "network": "Download method", "prefix": "Acceleration URL",
             "choices": ("Direct", "Use environment proxy", "GitHub acceleration"), "retry": "Retry", "back": "Back to overview",
             "start": "Start core", "ready": "default configuration is ready", "apply": "Apply"})


def exercise_options(terminal, output, name, labels, args):
    terminal.click_text(labels["more"])
    focus_control(terminal, labels["method"])
    terminal.key(b"\r")
    terminal.wait(labels["network"])
    for label in labels["choices"]:
        terminal.wait(label)
    capture(terminal, output, name + "-network-direct", args)
    terminal.key(b"\x1b[C")  # direct -> environment
    capture(terminal, output, name + "-network-environment", args)
    terminal.key(b"\x1b[C")  # environment -> accelerated
    terminal.wait(labels["prefix"])
    terminal.key(b"\t")
    prefix = "https://pty.example.invalid/"
    terminal.key(prefix.encode("ascii"))
    terminal.wait(prefix)
    capture(terminal, output, name + "-network-accelerated", args)
    terminal.key(b"\t")
    terminal.key(b"\r")
    terminal.wait("https://github.com/")
    for _ in range(12):
        if "version.txt" in terminal.screen.text():
            break
        terminal.key(b"\x1b[B")
    terminal.wait("version.txt")
    capture(terminal, output, name + "-network-url-preview", args)
    terminal.key(b"\x1b[Z")
    settle(terminal)
    for columns, rows in ((60, 20), (80, 24), (120, 32)):
        terminal.resize(columns, rows)
        settle(terminal)
        terminal.wait(prefix)
        if not any("›" in line and prefix in line for line in terminal.screen.text().splitlines()):
            raise AssertionError("Network resize lost acceleration input focus")
        capture(terminal, output, name + "-network-resize-%dx%d" % (columns, rows), args)
    terminal.key(b"\x1b")
    terminal.wait_absent(labels["prefix"])
    # Reopening must restore the applied direct method and discard the draft.
    focus_control(terminal, labels["method"])
    terminal.key(b"\r")
    terminal.wait(labels["network"])
    terminal.wait_absent(prefix)
    terminal.wait_absent(labels["prefix"])
    capture(terminal, output, name + "-network-discarded", args)
    terminal.key(b"\r")  # Applying environment mode requests a fresh read-only check.
    focus_control(terminal, labels["apply"])
    terminal.key(b"\r")
    terminal.wait_absent(labels["choices"][2])
    terminal.wait("v1.19.33")
    capture(terminal, output, name + "-network-applied", args)


def run(binary, output, language, profile, sizes, args, scenario):
    labels = words(language)
    with tempfile.TemporaryDirectory(prefix="lazyclash-onboarding-pty-") as temporary:
        home = Path(temporary) / "home"
        for columns, rows in sizes:
            name = "%s-%s-%sx%s-%s" % (language, profile, columns, rows, scenario)
            arguments = ["--lang", language, "--home", str(home), "--service-name", "lazyclash-onboarding-" + uuid.uuid4().hex[:12]]
            report = output / (name + "-calls.json")
            wrapper = WRAPPER
            if args.test_binary:
                environment = {"LAZYCLASH_INSTALL_PTY_REPORT": str(report), "LAZYCLASH_INSTALL_PTY_SCENARIO": scenario}
                wrapper = WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])", "os.environ.update(" + repr(environment) + "); os.environ['LAZYCLASH_INSTALL_PTY_LANGUAGE']=sys.argv[4]; os.execv(sys.argv[5], sys.argv[5:])")
                arguments = ["-test.run=^TestInstallFlowPTYFixture$", "-test.count=1"]
            command = [sys.executable, "-c", wrapper, str(columns), str(rows), profile, language, str(binary), *arguments]
            terminal = SMOKE.Terminal(command)
            terminal.screen = StyledScreen(columns, rows)
            expected = {"checks": 1, "resolves": 0, "prepares": 0, "installs": 0, "starts": 0}
            try:
                terminal.wait(labels["title"])
                terminal.wait(labels["later"])
                if args.test_binary:
                    terminal.wait("Fixture version check failed" if scenario == "check-retry" else "v1.19.33")
                capture(terminal, output, name + "-initial", args)
                screen = terminal.screen.text()
                if "1 / 3" in screen or "[1 " in screen or labels["routing"] in screen:
                    raise AssertionError("First-run inline guidance retained mandatory steps or unavailable cards")
                if labels["quit"] not in screen.splitlines()[-1]:
                    raise AssertionError("Quit hint is not in the final terminal row")
                if scenario == "options":
                    exercise_options(terminal, output, name, labels, args)
                    expected["checks"] = 2
                    terminal.resize(columns, rows)
                elif args.test_binary:
                    if scenario == "check-retry":
                        focus_control(terminal, labels["retry"])
                        terminal.key(b"\r")
                        terminal.wait("v1.19.33")
                        terminal.wait_absent("Fixture version check failed")
                        expected["checks"] = 2
                        capture(terminal, output, name + "-retried", args)
                        focus_control(terminal, labels["install"])
                    # No second page or confirmation: the first default Enter
                    # submits exactly one resolve -> prepare -> install chain.
                    terminal.key(b"\r")
                    terminal.wait(labels["downloading"])
                    capture(terminal, output, name + "-download", args)
                    expected.update(resolves=1, prepares=1, installs=1)
                    if scenario in ("success", "check-retry"):
                        terminal.wait(labels["start"])
                        terminal.wait(labels["ready"])
                        capture(terminal, output, name + "-success", args)
                    else:
                        terminal.key(b"\x1b")
                        terminal.wait(labels["cancelled"])
                        capture(terminal, output, name + "-cancelled", args)
                    focus_control(terminal, labels["back"])
                    terminal.key(b"\r")
                if scenario == "options" or not args.test_binary:
                    focus_control(terminal, labels["later"])
                    terminal.key(b"\r")
                terminal.wait_absent(labels["title"])
                terminal.wait(labels["routing"])
                capture(terminal, output, name + "-overview", args)
                terminal.key(b"3")
                terminal.wait(labels["profiles"])
                capture(terminal, output, name + "-profiles", args)
                terminal.exit()
                assert_uninstalled(home)
                if args.test_binary:
                    calls = json.loads(report.read_text())
                    if calls != expected:
                        raise AssertionError("Installation flow duplicated or skipped a backend stage: expected %r, got %r" % (expected, calls))
                raw = bytes(terminal.raw)
                (output / (name + ".ansi")).write_bytes(raw)
                if OSC_SET.search(raw):
                    raise AssertionError("TUI changed terminal foreground/background settings")
                if profile == "truecolor" and not RGB.search(raw):
                    raise AssertionError("True Color run emitted no RGB styles")
                if profile == "256" and (RGB.search(raw) or not INDEXED.search(raw)):
                    raise AssertionError("256-color run used unsupported color styling")
                if profile == "no-color" and any(part not in (b"", b"0") for match in SGR.finditer(raw) for part in match.group(1).split(b";")):
                    raise AssertionError("NO_COLOR run emitted styling")
                print("PASS:", name, expected, flush=True)
            except Exception:
                (output / (name + "-failed.txt")).write_text(terminal.screen.text() + "\n")
                (output / (name + "-failed.ansi")).write_bytes(bytes(terminal.raw))
                if args.png:
                    png(terminal.screen, output / (name + "-failed.png"), args.font, args.cjk_font, args.font_size)
                raise
            finally:
                terminal.cleanup()


def capture_arguments(parser):
    parser.add_argument("--png", action="store_true", help="Also redraw actual PTY cell states as PNG (requires Pillow)")
    parser.add_argument("--font", type=Path, help="Monospace TTF/TTC; defaults to Menlo or DejaVu Sans Mono")
    parser.add_argument("--cjk-font", type=Path, help="Chinese TTF/TTC; defaults to installed PingFang or Noto CJK")
    parser.add_argument("--font-size", type=int, default=18)


def configure_fonts(parser, args):
    if not args.png:
        return
    from PIL import Image  # noqa: F401
    fonts = [args.font] if args.font else [Path("/System/Library/Fonts/Menlo.ttc"), Path("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf")]
    args.font = next((font for font in fonts if font.is_file()), None)
    if args.font is None:
        parser.error("--font must select an existing monospace TTF/TTC file")
    cjk_fonts = ([args.cjk_font] if args.cjk_font else
                 [Path("/System/Library/Fonts/PingFang.ttc"),
                  *Path("/System/Library/AssetsV2").glob("com_apple_MobileAsset_Font*/*/AssetData/PingFang.ttc"),
                  Path("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"),
                  Path("/System/Library/Fonts/Supplemental/Songti.ttc")])
    args.cjk_font = next((font for font in cjk_fonts if font.is_file()), None)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--test-binary", action="store_true", help="Use an opt-in TUI test binary with in-memory install/check fixture; no real network, core or service")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--lang", choices=["en", "zh-Hans"], action="append", help="Repeat to select languages; default: both")
    parser.add_argument("--profile", choices=["truecolor", "256", "no-color"], action="append", help="Repeat to select capabilities; default: all")
    parser.add_argument("--size", action="append", help="Repeat COLSxROWS; default: 160x45,120x32,80x24,60x20,120x18")
    parser.add_argument("--scenario", choices=["cancel", "success", "check-retry", "options"], action="append", help="Fixture cases; default: cancel. Success/check-retry/options require --test-binary")
    capture_arguments(parser)
    args = parser.parse_args()
    sizes = []
    for value in args.size or []:
        try:
            size = tuple(int(value) for value in value.lower().split("x"))
            if len(size) != 2 or min(size) < 12:
                raise ValueError()
            sizes.append(size)
        except ValueError:
            parser.error("--size must be COLSxROWS with both dimensions at least 12")
    configure_fonts(parser, args)
    if not args.test_binary and any(scenario != "cancel" for scenario in args.scenario or []):
        parser.error("extra --scenario cases require --test-binary")
    binary = args.binary.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error("--binary must select an already built executable")
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    output.mkdir(parents=True)
    for language in args.lang or ["en", "zh-Hans"]:
        for profile in args.profile or ["truecolor", "256", "no-color"]:
            for scenario in args.scenario or ["cancel"]:
                run(binary, output, language, profile, sizes or SIZES, args, scenario)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
