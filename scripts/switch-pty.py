#!/usr/bin/env python3
"""Verify the Switch component in a real controlling PTY.

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
    environment = {"LAZYCLASH_SWITCH_PTY_REPORT": str(report)}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(args.binary.resolve()), "-test.run=^TestSwitchPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    on, off, pending, unknown = (("● Running", "○ Stopped", "Switching…", "Unknown")
        if language == "en" else ("● 运行中", "○ 已停止", "切换中…", "状态未知"))

    def row(index):
        return "".join(terminal.screen.cells[index])

    try:
        terminal.wait("focus=0 calls=0,0")
        terminal.settle()
        assert "›" in row(2) and "██░░░░" in row(2), row(2)
        assert on in row(3) and "›" not in row(3), row(3)
        preview.capture(terminal, output, "initial", args)

        terminal.key(b" ")
        terminal.wait("pending=true,false")
        terminal.key(b"\r ")
        assert "focus=0 calls=1,0" in terminal.screen.text()
        assert "actual=false,true" in terminal.screen.text()
        preview.capture(terminal, output, "slider-pending", args)
        terminal.wait("actual=true,true")
        assert "░░░░██" in row(2), row(2)

        # Clicking the state text moves focus and starts one operation. Simulate
        # a failed write, then retry it with the keyboard.
        terminal.key(b"f")
        terminal.click_text(on)
        terminal.wait("pending=false,true")
        terminal.wait("◌ " + pending)
        terminal.click_text(pending)
        terminal.key(b" ")
        assert "focus=1 calls=1,1" in terminal.screen.text()
        assert "actual=true,true" in terminal.screen.text()
        preview.capture(terminal, output, "status-pending", args)
        terminal.wait("failures=1")
        assert on in row(3) and "›" in row(3), row(3)
        terminal.key(b"\r")
        terminal.wait("actual=true,false")
        assert off in row(3), row(3)

        terminal.key(b"?")
        terminal.wait(unknown)
        terminal.click_text(unknown)
        terminal.key(b"\r ")
        terminal.settle()
        assert "focus=1 calls=1,2" in terminal.screen.text() and unknown in row(3)
        terminal.key(b"r")
        terminal.wait(off)
        terminal.key(b" ")
        terminal.wait("pending=false,true")
        terminal.key(b"r")
        terminal.wait("stale=1")
        assert "actual=true,false" in terminal.screen.text() and off in row(3)

        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            assert row(2).endswith("░░░░██") and "›" not in row(2), row(2)
            assert off in row(3) and "›" in row(3), row(3)
            preview.capture(terminal, output, "switch-%dx%d" % (width, height), args)
        terminal.exit()
        result = json.loads(report.read_text())
        assert result == {"calls": [1, 3], "stale": 1, "failures": 1}, result
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "fixture changed terminal defaults"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styles"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: Slider/Status, keyboard and mouse, deduplication, failed write, stale read, resize; %s" %
              (language, profile, json.dumps(result)), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
        raise
    finally:
        terminal.cleanup()


def main():
    import test_artifacts

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, help="Use an existing TUI test binary (default: build a temporary one)")
    parser.add_argument("--lang", choices=["en", "zh-Hans"], action="append")
    parser.add_argument("--profile", choices=["truecolor", "256", "no-color"], action="append")
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("switch_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    test_artifacts.run_pty(parser, run_suite, preview)


def run_suite(args, parser, preview):
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "256", "no-color"):
            exercise(preview, args, output / language / profile, language, profile)


if __name__ == "__main__":
    main()
