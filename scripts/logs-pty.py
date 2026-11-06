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


def assert_stream(terminal, report, streams, canceled=0, failures=0):
    current = calls(report)
    assert current["streams"] == streams and current["canceled"] == canceled and current["failures"] == failures, current
    assert not re.search(r"\bGAP\b", terminal.screen.text()), "stream lifecycle leaked a synthetic GAP log entry"


def assert_continuing(terminal, report, seconds=0.8):
    before = calls(report)
    observe(terminal, seconds)
    after = calls(report)
    assert after["emitted"] > before["emitted"], "background collection stopped"
    assert_stream(terminal, report, before["streams"], before["canceled"], before["failures"])


def wait_long_payload(terminal):
    terminal.settle()
    terminal.wait("event-057")
    terminal.wait("LONG-MESSAGE-")
    left, _ = position(terminal, "event-057")
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
        wait_until(terminal, lambda: calls(report).get("emitted", 0) >= 60, "session did not collect logs before opening Logs")
        assert_continuing(terminal, report)
        preview.capture(terminal, output, "collecting-from-overview", args)
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

        # Navigation, a read-only busy refresh, help and a canceled form keep
        # the session's collector alive instead of replacing its HTTP request.
        terminal.key(b"1")
        terminal.settle()
        before = calls(report)
        terminal.key(b"g")
        wait_until(terminal, lambda: calls(report).get("snapshots", 0) > before["snapshots"], "Overview refresh did not run")
        assert_continuing(terminal, report)
        assert_stream(terminal, report, 1)
        terminal.key(b"6")
        terminal.key(b"\x1b[F")
        terminal.settle()
        terminal.key(b"?")
        terminal.wait(words["help.currentPage"].split("%s")[0])
        assert_continuing(terminal, report)
        preview.capture(terminal, output, "help-continuing-stream", args)
        terminal.key(b"1234567")
        terminal.key(b"?")
        wait_until(terminal, lambda: words["help.currentPage"].split("%s")[0] not in terminal.screen.text(), "help did not close")
        terminal.settle()
        terminal.key(b"n")
        terminal.wait(words["wizard.network.title"])
        assert_continuing(terminal, report)
        preview.capture(terminal, output, "form-continuing-stream", args)
        terminal.key(b"\x1b")
        wait_until(terminal, lambda: words["wizard.network.title"] not in terminal.screen.text(), "network form did not close")
        terminal.settle()
        assert_stream(terminal, report, 1)

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

        # Pause freezes the visible snapshot, while a large incoming burst
        # still enters the bounded cache. Resume reveals only the newest 200.
        terminal.key(b"f")
        terminal.wait(words["logs.resume_display"])
        terminal.settle()
        frozen = event_numbers(terminal)
        assert frozen, "pause lost the visible snapshot"
        (output / "burst-events").touch()
        wait_until(terminal, lambda: calls(report).get("burst_events") == 230, "paused view stopped the background burst")
        assert_continuing(terminal, report)
        assert event_numbers(terminal) == frozen, "paused display changed after cache eviction"
        assert_stream(terminal, report, 1)
        preview.capture(terminal, output, "display-paused-cache-continues", args)
        terminal.key(b" ")
        terminal.wait(words["logs.pause_display"])
        wait_until(terminal, lambda: event_numbers(terminal) and max(event_numbers(terminal)) > max(frozen) + 200, "Space did not resume the latest cached entries")
        terminal.wait(words["logs.summary"] % (200, 200))
        terminal.key(b"\x1b[H")
        terminal.settle()
        assert min(event_numbers(terminal)) > max(frozen), "resumed cache retained evicted old entries"
        preview.capture(terminal, output, "resumed-cache-limit", args)
        terminal.key(b"\x1b[F")
        terminal.settle()
        terminal.click_text(words["logs.pause_display"])
        terminal.wait(words["logs.resume_display"])
        frozen = event_numbers(terminal)
        assert_continuing(terminal, report)
        assert event_numbers(terminal) == frozen, "Pause button failed to freeze display"
        terminal.key(b"\x1b[F")
        terminal.wait(words["logs.pause_display"])
        assert_stream(terminal, report, 1)
        preview.capture(terminal, output, "end-resumes-without-reconnect", args)

        # Clear affects the local buffer. The owned request must stay connected.
        before = calls(report)
        terminal.key(b"\x0c")
        wait_until(terminal, lambda: calls(report).get("emitted", 0) > before["emitted"], "clear stopped incoming events")
        terminal.settle()
        after = calls(report)
        assert_stream(terminal, report, 1)
        assert "event-000" not in terminal.screen.text(), "clear retained old entries"
        assert all(value >= before["emitted"] for value in event_numbers(terminal)), "clear retained old event IDs"
        preview.capture(terminal, output, "clear-continuing-stream", args)
        terminal.key(b"g")
        wait_until(terminal, lambda: calls(report).get("streams") == 2, "manual reconnect did not replace the request")
        assert_continuing(terminal, report)
        assert_stream(terminal, report, 2, 1)
        preview.capture(terminal, output, "manual-reconnect-once", args)

        # A single real backend error must recover without pressing g, and the
        # interruption belongs to status rather than an invented log record.
        (output / "break-stream").touch()
        wait_until(terminal, lambda: calls(report).get("failures") == 1, "fixture did not interrupt the active request")
        terminal.wait(words["logs.reconnecting"])
        preview.capture(terminal, output, "interrupted-retrying-status", args)
        wait_until(terminal, lambda: calls(report).get("streams") == 3, "interrupted stream did not reconnect automatically")
        assert_continuing(terminal, report)
        assert_stream(terminal, report, 3, 1, 1)
        preview.capture(terminal, output, "automatic-recovery-no-gap-entry", args)
        search(terminal, "GAP")
        terminal.wait(words["logs.no_matches"])
        preview.capture(terminal, output, "no-synthetic-gap-record", args)
        search(terminal, "")
        terminal.exit()
        final = calls(report)
        assert final["streams"] == 3 and final["canceled"] == 2 and final["failures"] == 1 and final["burst_events"] == 230, final
        assert final["service_changes"] == final["modes"] == final["selections"] == 0, final
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "application changed the terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: detailed/compact, local search/levels, session capture across navigation/help/form, frozen display with 200-entry live cache, fixed toolbar at five sizes, clear/manual reconnect/automatic recovery without GAP entries; %s" %
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
