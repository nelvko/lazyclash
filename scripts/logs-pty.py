#!/usr/bin/env python3
"""Verify the Logs page in a real controlling PTY with synthetic live events.

Build: go test -c ./internal/tui -o bin/logs-tui.test
No managed data, core, network or service is used. PNGs redraw captured VT cells
and colors; they are not native terminal-app screenshots.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys
import time
import uuid


SIZES = ((160, 45), (120, 32), (80, 24), (60, 20), (40, 16), (160, 45))


def labels(language):
    result = {}
    directory = Path(__file__).resolve().parents[1] / "internal" / "i18n" / "locales"
    for path in directory.glob(language + ".*.json"):
        result.update(json.loads(path.read_text()))
    return result


def wait_until(terminal, predicate, label, timeout=10):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        terminal.read(0.04)
        if not terminal.screen.synchronized and terminal.screen.state == "text" and predicate():
            return
        if terminal.closed:
            break
    raise AssertionError(label + "\n" + terminal.screen.text())


def observe(terminal, seconds):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        terminal.read(0.04)
    terminal.settle()


def calls(report):
    return json.loads(report.read_text()) if report.exists() else {}


def position(terminal, label):
    for y, row in enumerate(terminal.screen.cells):
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(label):
                return x, y
    raise AssertionError("Visible text missing: " + label + "\n" + terminal.screen.text())


def event_numbers(terminal):
    return [int(value) for value in re.findall(r"event-(\d+)", terminal.screen.text())]


def wait_long_payload(terminal):
    terminal.settle()
    terminal.wait("event-057")
    left, _ = position(terminal, "/ long-message-end")
    wait_until(terminal, lambda: "LONG-MESSAGE-END" in "".join(
        "".join(row[left:]).strip() for row in terminal.screen.cells), "detailed view lost the end of the wrapped payload")


def search(terminal, value):
    terminal.key(b"/")
    terminal.key(b"\x05")
    terminal.key(b"\x15")
    terminal.key(b"\x1b[200~" + value.encode() + b"\x1b[201~")
    terminal.key(b"\r")
    terminal.settle()


def fixture_terminal(preview, binary, report, language, profile):
    environment = {"LAZYCLASH_LOGS_PTY_REPORT": str(report), "LAZYCLASH_LOGS_PTY_LANGUAGE": language}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestLogsPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"6")
        terminal.wait("event-059")
        terminal.settle()
        preview.capture(terminal, output, "logs-wide", args)
        rows = terminal.screen.text().splitlines()
        _, message_y = position(terminal, "event-059")
        assert re.search(r"\d\d:\d\d:\d\d.*ERROR", rows[message_y - 1]), "detailed metadata is not above its message"

        # Reading history freezes the viewport while the same stream continues.
        terminal.key(b"\x1b[H")
        terminal.wait("event-000")
        terminal.settle()
        retained = event_numbers(terminal)
        before = calls(report)
        observe(terminal, 1.2)
        after = calls(report)
        assert event_numbers(terminal) == retained, "new events pulled the browsing viewport to the tail"
        assert after["emitted"] > before["emitted"] and after["streams"] == 1 and after["canceled"] == 0, after
        preview.capture(terminal, output, "browsing-live-stream", args)

        # Filters operate on retained events without reconnecting or pausing.
        terminal.key(b"/")
        terminal.key(b"\x05")
        terminal.key(b"\x15")
        terminal.key(b"q1234567-no-such-log")
        terminal.key(b"\r")
        terminal.settle()
        terminal.wait(words["logs.no_matches"])
        assert calls(report)["streams"] == 1 and calls(report)["canceled"] == 0, calls(report)
        preview.capture(terminal, output, "no-matches-input-isolation", args)
        search(terminal, "case-needle")
        terminal.wait("Case-Needle")
        assert "ERROR-MARKER" not in terminal.screen.text(), "search retained unrelated messages"
        preview.capture(terminal, output, "case-insensitive-search", args)
        search(terminal, "")
        terminal.key(b"\x1b[F")
        terminal.settle()
        for expected, absent in (("INFO-MARKER", ("WARNING-MARKER", "ERROR-MARKER")),
                                 ("WARNING-MARKER", ("INFO-MARKER", "ERROR-MARKER")),
                                 ("ERROR-MARKER", ("INFO-MARKER", "WARNING-MARKER"))):
            terminal.key(b"l")
            terminal.wait(expected)
            terminal.settle()
            assert all(value not in terminal.screen.text() for value in absent), "level filter retained an unrelated level"
            preview.capture(terminal, output, "filter-" + expected.lower(), args)
        terminal.key(b"l")  # All.
        terminal.settle()

        # Detailed mode exposes the full wrapped payload; compact mode crops it.
        search(terminal, "long-message-end")
        wait_long_payload(terminal)
        preview.capture(terminal, output, "detailed-long-message", args)
        terminal.key(b"v")
        terminal.wait_absent("LONG-MESSAGE-")
        terminal.wait("event-057")
        preview.capture(terminal, output, "compact-long-message", args)
        terminal.key(b"v")
        wait_long_payload(terminal)
        search(terminal, "")

        # Both the fixed search and level controls stay in place during wheel
        # scrolling, including narrow terminals and restoring the original size.
        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.key(b"\x1b[<35;1;1M")
            terminal.settle()
            terminal.key(b"\x1b[H")
            terminal.wait("event-000")
            terminal.settle()
            placeholder = words["logs.placeholder"]
            # A narrow input shows only the beginning of the placeholder.
            toolbar = position(terminal, placeholder[:min(5, len(placeholder))])
            old = event_numbers(terminal)
            for _ in range(5):
                terminal.key(("\x1b[<65;%d;%dM" % (width - 3, height // 2)).encode())
            terminal.settle()
            assert old != event_numbers(terminal), "wheel did not scroll the log viewport"
            assert toolbar == position(terminal, placeholder[:min(5, len(placeholder))]), "search field moved with the list"
            preview.capture(terminal, output, "scrolled-%dx%d" % (width, height), args)
            terminal.key(b"\x1b[F")
            terminal.settle()
            preview.capture(terminal, output, "following-%dx%d" % (width, height), args)
        assert calls(report)["streams"] == 1 and calls(report)["canceled"] == 0, calls(report)

        # Only an explicit capture pause disconnects; resume discloses the gap.
        for _ in range(3):
            terminal.key(b"l")  # Error-only filtering must not hide a capture gap.
        terminal.settle()
        terminal.key(b"f")
        wait_until(terminal, lambda: calls(report).get("canceled") == 1, "pause did not cancel capture")
        terminal.settle()
        paused = calls(report)
        observe(terminal, 1.0)
        assert calls(report)["emitted"] == paused["emitted"], calls(report)
        preview.capture(terminal, output, "capture-paused", args)
        terminal.key(b"\x1b[F")
        wait_until(terminal, lambda: calls(report).get("streams") == 2, "End did not resume capture")
        terminal.wait(words["logs.gap"])
        preview.capture(terminal, output, "capture-resumed-gap", args)
        terminal.key(b"l")  # All.
        terminal.settle()

        # Clear affects the local buffer. The owned request must stay connected.
        before = calls(report)
        terminal.key(b"\x0c")
        wait_until(terminal, lambda: calls(report).get("emitted", 0) > before["emitted"], "clear stopped incoming events")
        terminal.settle()
        after = calls(report)
        assert after["streams"] == 2 and after["canceled"] == 1, after
        assert "event-000" not in terminal.screen.text() and words["logs.gap"] not in terminal.screen.text(), "clear retained old entries"
        assert all(value >= before["emitted"] for value in event_numbers(terminal)), "clear retained old event IDs"
        preview.capture(terminal, output, "clear-continuing-stream", args)
        terminal.key(b"g")
        wait_until(terminal, lambda: calls(report).get("streams") == 3, "reconnect did not replace the request")
        terminal.exit()
        final = calls(report)
        assert final["streams"] == 3 and final["canceled"] == 3, final
        assert final["service_changes"] == final["modes"] == final["selections"] == 0, final
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "application changed the terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: detailed/compact, local search/levels, continuous capture while browsing, fixed toolbar across five dimensions, pause/gap/clear/reconnect; %s" %
              (language, profile, json.dumps(final)), flush=True)
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
    spec = importlib.util.spec_from_file_location("logs_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor",):
            exercise(preview, args.binary, output / language / profile, language, profile, args)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
