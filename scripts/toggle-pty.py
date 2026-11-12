#!/usr/bin/env python3
"""Verify the Toggle component in a real controlling PTY.

Build: go test -c ./internal/tui -o bin/toggle-tui.test
Uses only a component fixture with delayed observations, no backend or services.
Optional PNGs redraw actual PTY character cells and attributes.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sys
import time
import uuid


SIZES = ((160, 45), (120, 32), (80, 24), (60, 20), (40, 16), (160, 45))


def exercise(preview, args, output, language, profile):
    output.mkdir(parents=True)
    report = output / "result.json"
    environment = {"LAZYCLASH_TOGGLE_PTY_REPORT": str(report)}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(args.binary.resolve()), "-test.run=^TestTogglePTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    labels = (["● On", "○ Off", "◌ Switching…", "Unavailable", "Unsupported", "Unknown"]
              if language == "en" else
              ["● 已开启", "○ 已关闭", "◌ 切换中…", "不可用", "暂不支持", "状态未知"])
    try:
        terminal.wait("focus=0 calls=0,0 stale=0")
        terminal.settle()
        for y, label in enumerate(labels, 2):
            assert "".join(terminal.screen.cells[y]).rstrip().endswith(label), terminal.screen.text()
        preview.capture(terminal, output, "initial", args)

        terminal.key(b" ")
        terminal.wait("state=3,1 last=2,1")
        terminal.key(b"\r ")
        terminal.wait("calls=1,0")
        preview.capture(terminal, output, "pending", args)
        terminal.wait("state=1,1 last=1,1")
        terminal.key(b"\t")
        terminal.wait("focus=1 calls=1,0")
        terminal.key(b"\r")
        terminal.wait("state=1,3 last=1,1")
        terminal.wait("state=1,2 last=1,2")
        terminal.wait("calls=1,1")

        terminal.key(b"\t\r")
        terminal.wait("state=3,2 last=1,2")
        terminal.key(b"r")
        terminal.wait("state=2,2 last=2,2")
        terminal.wait("stale=1")
        for key, state in ((b"u", 4), (b"s", 5), (b"?", 0)):
            terminal.key(key)
            terminal.wait("state=%d,2 last=2,2" % state)
            terminal.key(b"\r ")
            terminal.settle()
            terminal.wait("calls=2,1 stale=1")
            assert "state=%d,2 last=2,2" % state in terminal.screen.text(), "disabled state activated"
        terminal.key(b"r")
        terminal.wait("state=2,2 last=2,2")

        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            for y, label in enumerate(labels, 2):
                row = "".join(terminal.screen.cells[y]).rstrip()
                assert row.endswith(label), "state lost on resize: " + row
                assert terminal.screen.cells[y][-1] != " ", "toggle is not right aligned"
            terminal.wait("focus=0 calls=2,1 stale=1")
            preview.capture(terminal, output, "toggle-%dx%d" % (width, height), args)

        # Read colors and focus from the terminal cell state, not the model.
        if profile == "truecolor":
            for y, foreground in enumerate(("#99c794", "#a9afbc", "#85b3e0", "#7b8896", "#7b8896", "#7b8896"), 2):
                x = next(x for x in range(2, terminal.screen.columns) if terminal.screen.cells[y][x] not in ("", " "))
                actual = terminal.screen.styles[y][x][0]
                if not isinstance(actual, str):
                    actual = "#%02x%02x%02x" % tuple(actual)
                assert actual.lower() == foreground, (y, actual, foreground)
        if profile != "no-color":
            assert terminal.screen.styles[9][-1][3], "focused control has no underline"
            assert not terminal.screen.styles[10][-1][3], "unfocused peer retained underline"
        preview.capture(terminal, output, "final", args)
        terminal.exit()
        result = json.loads(report.read_text())
        assert result == {"calls": [2, 1], "stale": 1, "states": [2, 2]}, result
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "fixture changed terminal defaults"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styles"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: states, pending, focus routing, blocked input, stale results, resize; %s" %
              (language, profile, json.dumps(result)), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
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
    spec = importlib.util.spec_from_file_location("toggle_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
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
