#!/usr/bin/env python3
"""Exercise the shared button component in a real controlling PTY.

Build: go test -c ./internal/tui -o bin/buttons-tui.test
The explicit fixture has no backend, network, managed data or services. PNGs
redraw actual terminal cells and attributes, rather than a native app window.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sys
import time
import uuid


SIZES = ((160, 45), (120, 32), (80, 24), (60, 20), (40, 16), (160, 45))


def labels(language):
    return (["规则", "全局", "直连", "应用设置", "重启", "删除配置", "确认删除", "查看详情", "不可操作"]
            if language == "zh-Hans" else
            ["Rule", "Global", "Direct", "Apply settings", "Restart", "Delete profile", "Delete forever", "Details", "Unavailable"])


def position(terminal, label):
    for y, row in enumerate(terminal.screen.cells):
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(label):
                return x, y
    raise AssertionError("Missing button: " + label + "\n" + terminal.screen.text())


def color(value):
    return value.lower() if isinstance(value, str) else "#%02x%02x%02x" % tuple(value)


def background(terminal, point):
    x, y = point
    return color(terminal.screen.styles[y][x][1])


def click(terminal, point):
    x, y = point
    terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())
    terminal.settle()


def hover(terminal, point):
    x, y = point
    terminal.key(("\x1b[<35;%d;%dM" % (x + 1, y + 1)).encode())
    terminal.settle()


def fixture_terminal(preview, binary, report, language, profile):
    environment = {"LAZYCLASH_BUTTONS_PTY_REPORT": str(report), "LAZYCLASH_BUTTONS_PTY_LANGUAGE": language}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestButtonsPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def exercise(preview, args, output, language, profile):
    output.mkdir(parents=True)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, args.binary, report, language, profile)
    words = labels(language)
    try:
        terminal.wait("selected=global focus=global")
        terminal.settle()
        initial = [position(terminal, label) for label in words]
        if profile == "truecolor":
            assert background(terminal, initial[1]) == "#425d78", "selected mode lost its background"
            assert background(terminal, initial[4]) == "#3a424a", "secondary button has the wrong background"
            x, y = initial[1]
            selected_bg = background(terminal, (x, y))
            while x > 0 and background(terminal, (x - 1, y)) == selected_bg:
                x -= 1
            assert terminal.screen.cells[y][x - 1] == "›", "focus arrow is separated from its button"
            assert terminal.screen.cells[y][x - 2] == " ", "focus gutter has no leading gap"
        elif profile == "no-color":
            assert "<" in terminal.screen.text() and ">" in terminal.screen.text(), "selection lacks its textual boundary"
            assert " ›<" in terminal.screen.text(), "focus arrow no longer touches its selected boundary"
        preview.capture(terminal, output, "initial", args)

        terminal.key(b"\x1b[C")
        terminal.wait("selected=global focus=direct")
        terminal.settle()
        assert [position(terminal, label) for label in words] == initial, "focus moved button labels"
        preview.capture(terminal, output, "focus-independent", args)

        hover(terminal, initial[4])
        terminal.wait("hover=restart")
        if profile == "truecolor":
            assert background(terminal, initial[4]) == "#46515c", "secondary hover is not visible"
            assert background(terminal, initial[1]) == "#425d78", "hover changed the selected mode"
        preview.capture(terminal, output, "hover-secondary", args)
        hover(terminal, initial[1])
        if profile == "truecolor":
            assert background(terminal, initial[1]) == "#46617b", "selected hover lost its own state color"
        preview.capture(terminal, output, "hover-selected", args)
        hover(terminal, initial[5])
        if profile == "truecolor":
            assert background(terminal, initial[5]) == "#493940", "danger hover reused a neutral background"
        preview.capture(terminal, output, "hover-danger", args)
        hover(terminal, initial[3])
        if profile == "truecolor":
            assert background(terminal, initial[3]) == "#6aa4df", "primary hover changed its color role"
        preview.capture(terminal, output, "hover-primary", args)
        hover(terminal, initial[6])
        if profile == "truecolor":
            assert background(terminal, initial[6]) == "#f5a3a8", "danger confirmation hover lost its color role"
        preview.capture(terminal, output, "hover-confirmation", args)
        hover(terminal, initial[7])
        if profile != "no-color":
            x, y = initial[7]
            assert terminal.screen.styles[y][x][3], "text button hover lost its underline"
        preview.capture(terminal, output, "hover-text", args)

        click(terminal, initial[2])
        terminal.wait("selected=direct focus=direct")
        assert [position(terminal, label) for label in words] == initial, "selection moved labels or adjacent buttons"
        terminal.key(b"b")
        terminal.wait("busy=true")
        click(terminal, initial[0])
        click(terminal, initial[3])
        terminal.wait("selected=direct focus=direct")
        if profile == "truecolor":
            assert background(terminal, initial[2]) == "#425d78", "busy selection disappeared"
        preview.capture(terminal, output, "busy", args)
        terminal.key(b"b")
        terminal.key(b"d")
        terminal.wait("disabled=true")
        click(terminal, initial[0])
        click(terminal, initial[3])
        terminal.wait("selected=direct focus=direct")
        preview.capture(terminal, output, "disabled", args)
        terminal.key(b"d")
        terminal.wait("disabled=false")

        for index in range(3, 9):
            click(terminal, initial[index])
        terminal.wait("focus=details")
        preview.capture(terminal, output, "all-roles", args)

        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            terminal.wait("selected=direct")
            for label in words:
                position(terminal, label)
            preview.capture(terminal, output, "buttons-%dx%d" % (width, height), args)

        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls == {"selected": "direct", "actions": {"apply": 1, "restart": 1, "delete": 1, "confirm": 1, "details": 1}}, calls
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "fixture changed terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: roles, geometry, focus/selection, hover, busy/disabled guards, resize; %s" % (language, profile, json.dumps(calls)), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
        if args.png:
            preview.png(terminal.screen, output / "failed.png", args.font, args.cjk_font, args.font_size)
        raise
    finally:
        terminal.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--lang", choices=["en", "zh-Hans"], action="append")
    parser.add_argument("--profile", choices=["truecolor", "256", "no-color"], action="append")
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("buttons_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "256", "no-color"):
            exercise(preview, args, output / language / profile, language, profile)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
