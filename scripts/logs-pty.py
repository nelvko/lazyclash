#!/usr/bin/env python3
"""Verify the Logs page in a real controlling PTY with synthetic live events.

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

from tui_layout import dropdown_option, dropdown_trigger


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


def assert_log_controls(terminal, words):
    header = "\n".join(terminal.screen.text().splitlines()[:3])
    assert words["header.reconnect"] not in header and "↻" not in header, "Logs exposed a reconnect button"
    assert words["header.live"] not in header, "Logs repeated its normal live status"


def assert_log_toolbar(terminal, words):
    x, y = position(terminal, words["logs.placeholder"][:5])
    row = terminal.screen.cells[y]
    left = max(i for i, cell in enumerate(row[:x + 1]) if cell == "│")
    right = next(i for i in range(x, len(row)) if row[i] == "│")
    trigger = dropdown_trigger(terminal, words["logs.level_all"])
    level_x, level_y = position(terminal, trigger)
    assert not "".join(row[right + 1:]).strip(), "unexpected control after the unfiltered search field"
    if level_y == y:
        assert level_x < left, "level must precede search"
    else:
        assert level_y < y - 1, "narrow scope must precede the complete search frame"
        scope_row = "".join(terminal.screen.cells[level_y][level_x:]).strip()
        assert re.fullmatch(re.escape(trigger) + r"(?: *\])?", scope_row), "unexpected control after the level dropdown"
    return left, right, y, level_x, level_y


def assert_log_toolbar_tab_order(terminal, words):
    # Level precedes the editor at every terminal width.
    terminal.key(b"/")
    terminal.key(b"\x1b[Z")
    terminal.key(b"\r")
    dropdown_option(terminal, words["logs.level_all"], checked=True)
    terminal.key(b"\x1b")
    terminal.wait_absent("✓")
    # Tab returns from the level control to the editor, which owns text.
    terminal.key(b"\t")
    terminal.key(b"\r")
    terminal.key(b"\x1b[200~tabok\x1b[201~")
    terminal.wait("tabok")
    terminal.key(b"\r")
    search(terminal, "")


def assert_no_log_summary(terminal):
    text = terminal.screen.text()
    assert "Mihomo logs" not in text and "Mihomo 日志" not in text, "Logs retained its permanent summary row"
    assert not re.search(r"(?:Local|本地) \d\d:\d\d:\d\d", text), "Logs retained its observed-time hint"


def choose_level(terminal, words, index, mouse=False):
    terminal.key(b"l")
    terminal.wait("✓")
    if mouse:
        target = words[("logs.level_all", "logs.level_info", "logs.level_warn", "logs.level_error")[index]]
        terminal.click_text(dropdown_option(terminal, target))
    else:
        terminal.key(b"\x1b[H")
        for _ in range(index):
            terminal.key(b"\x1b[B")
        terminal.key(b"\r")
    terminal.wait_absent("✓")
    terminal.settle()


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
    left, _ = position(terminal, "event-057")
    wait_until(terminal, lambda: "LONG-MESSAGE-END" in "".join(
        "".join(row[left:]).strip() for row in terminal.screen.cells), "word wrap lost the end of the payload")


def search(terminal, value):
    terminal.key(b"/")
    terminal.key(b"\x05")
    terminal.key(b"\x15")
    terminal.key(b"\x1b[200~" + value.encode() + b"\x1b[201~")
    terminal.key(b"\r")
    terminal.settle()


def wait_log_action(terminal, label):
    # Short Pause/暂停 labels must not match Paused/已暂停 in the status text.
    pattern = re.compile(r"(?<!\w)" + re.escape(label) + r"(?!\w)")
    wait_until(terminal, lambda: pattern.search(terminal.screen.text()) is not None,
               "log action missing: " + label)


def wait_log_footer(terminal, words, status_key, min_unread=0):
    status = words[status_key]
    latest = re.compile(r"\s+(?:›\s*)?\[?(?:" + re.escape(words["logs.latest"]) + r"|↓)(?: \+(\d+))?\]?$")

    def matches():
        # Exclude the sidebar and shortcut legend. Narrow terminals may crop
        # the left status and reduce the button to its arrow.
        row = "".join(terminal.screen.cells[-2]).strip()
        start = row.find(status[:5])
        if start < 0:
            return False
        row = row[start:]
        button = latest.search(row)
        visible_status = row[:button.start()] if button else row
        if not status.startswith(visible_status.rstrip().rstrip("…")):
            return False
        if status_key not in ("logs.browsing", "logs.display_paused"):
            return button is None
        return button is not None and int(button.group(1) or 0) >= min_unread

    wait_until(terminal, matches, "unexpected log status or Latest button: " + status_key)


def assert_no_connection_notice_in_list(terminal, words):
    body = "\n".join("".join(row) for row in terminal.screen.cells[3:-2])
    for text in (words["logs.reconnecting"], words["logs.recovered"]):
        assert text not in body, "connection notice appeared in the log list: " + text


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
        terminal.wait("Logs preview")
        wait_until(terminal, lambda: calls(report).get("emitted", 0) >= 60, "session did not collect logs before opening Logs")
        assert_continuing(terminal, report)
        preview.capture(terminal, output, "collecting-from-overview", args)
        terminal.key(b"6")
        terminal.wait("event-059")
        terminal.settle()
        assert_log_controls(terminal, words)
        wait_log_footer(terminal, words, "logs.capturing")
        assert_no_log_summary(terminal)
        assert not re.search(r"\d+ / \d+", terminal.screen.text()), "unfiltered Logs exposed a count"
        for label in ("Aa", "ab", ".*"):
            position(terminal, label)
        assert_log_toolbar(terminal, words)
        preview.capture(terminal, output, "logs-wide", args)
        terminal.click_text(dropdown_trigger(terminal, words["logs.level_all"]))
        dropdown_option(terminal, words["logs.level_all"], checked=True)
        assert_continuing(terminal, report)
        terminal.key(b"q123")
        dropdown_option(terminal, words["logs.level_all"], checked=True)
        preview.capture(terminal, output, "level-menu-capturing", args)
        terminal.key(b"\x1b")
        terminal.wait_absent("✓")
        rows = terminal.screen.text().splitlines()
        _, message_y = position(terminal, "event-059")
        assert re.search(r"\d\d:\d\d:\d\d.*ERROR.*event-059", rows[message_y]), "metadata and message are not on the same row"

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
        wait_log_footer(terminal, words, "logs.browsing", min_unread=1)
        preview.capture(terminal, output, "browsing-live-stream", args)
        terminal.click_text(words["logs.latest"])
        wait_log_footer(terminal, words, "logs.capturing")
        assert max(event_numbers(terminal)) > max(retained), "clicking Latest did not leave the historical entries"

        # Navigation, a read-only busy refresh, hidden hints and a canceled form keep
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
        terminal.wait_absent(words["dashboard.footer.quit"])
        terminal.wait(words["logs.capturing"])
        assert_continuing(terminal, report)
        preview.capture(terminal, output, "hints-hidden-continuing-stream", args)
        terminal.key(b"?")
        terminal.wait(words["dashboard.footer.quit"])
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
        terminal.key(b"?q1234567-no-such-log")
        terminal.key(b"\r")
        terminal.settle()
        terminal.wait(words["logs.no_matches"])
        assert calls(report)["streams"] == 1 and calls(report)["canceled"] == 0, calls(report)
        preview.capture(terminal, output, "no-matches-input-isolation", args)
        search(terminal, "case-needle")
        terminal.wait("event-054")
        terminal.wait("Case-Needle")
        assert "ERROR-MARKER" not in terminal.screen.text(), "search retained unrelated messages"
        assert re.search(r"1 / \d+", terminal.screen.text()), "search lost its conditional count"
        preview.capture(terminal, output, "case-insensitive-search", args)
        # Match controls share Rules' editor and remain local to the live cache.
        terminal.click_text("Aa")
        terminal.wait(words["logs.no_matches"])
        search(terminal, "Case-Needle")
        terminal.wait("event-054")
        terminal.key(b"\x1bc")  # Alt+C disables match case.
        search(terminal, "Need")
        terminal.wait("event-054")
        terminal.click_text("ab")
        terminal.wait(words["logs.no_matches"])
        search(terminal, "Needle")
        terminal.wait("event-054")
        preview.capture(terminal, output, "whole-word-search", args)
        terminal.key(b"\x1bw")  # Alt+W disables whole word.
        search(terminal, "Case-[N]eedle")
        terminal.wait(words["logs.no_matches"])
        terminal.click_text(".*")
        terminal.wait("event-054")
        search(terminal, "[")
        terminal.wait(words["logs.invalid_regex"])
        preview.capture(terminal, output, "invalid-regex-search", args)
        search(terminal, "Case-[N]eedle")
        terminal.wait("event-054")
        assert words["logs.invalid_regex"] not in terminal.screen.text(), "valid expression kept its error"
        preview.capture(terminal, output, "corrected-regex-search", args)
        terminal.key(b"\x1br")  # Alt+R returns to literal matching.
        search(terminal, "case-needle")
        terminal.wait("event-054")
        assert_continuing(terminal, report)
        search(terminal, "")
        terminal.key(b"\x1b[F")
        terminal.settle()
        for index, (expected, absent) in enumerate((("INFO-MARKER", ("WARNING-MARKER", "ERROR-MARKER")),
                                 ("WARNING-MARKER", ("INFO-MARKER", "ERROR-MARKER")),
                                 ("ERROR-MARKER", ("INFO-MARKER", "WARNING-MARKER"))), 1):
            choose_level(terminal, words, index, mouse=index == 2)
            terminal.wait(expected)
            terminal.settle()
            assert all(value not in terminal.screen.text() for value in absent), "level filter retained an unrelated level"
            preview.capture(terminal, output, "filter-" + expected.lower(), args)
        choose_level(terminal, words, 0)
        assert not re.search(r"\d+ / \d+", terminal.screen.text()), "clearing filters retained a count"
        terminal.settle()

        # Long payloads wrap by default. The shortcut clips them to one row
        # and restores the full text while keeping the same filtered entry.
        search(terminal, "long-message-end")
        terminal.resize(80, 24)
        wait_long_payload(terminal)
        rows = terminal.screen.text().splitlines()
        _, message_y = position(terminal, "event-057")
        assert re.search(r"\d\d:\d\d:\d\d.*INFO.*event-057", rows[message_y]), "wrapped metadata is not inline with its message"
        preview.capture(terminal, output, "default-wrapped-long-message", args)
        terminal.key(b"v")
        wait_until(terminal, lambda: re.search(r"event-057[^\n]*…", terminal.screen.text()), "v did not clip the long log to one line")
        terminal.wait_absent("中文长日志")
        preview.capture(terminal, output, "single-line-long-message", args)
        terminal.key(b"v")
        wait_long_payload(terminal)
        terminal.resize(160, 45)
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
            assert_log_controls(terminal, words)
            placeholder = words["logs.placeholder"]
            # A narrow input shows only the beginning of the placeholder.
            toolbar = position(terminal, placeholder[:min(5, len(placeholder))])
            option_geometry = assert_log_toolbar(terminal, words)
            old = event_numbers(terminal)
            for _ in range(5):
                terminal.key(("\x1b[<65;%d;%dM" % (width - 3, height // 2)).encode())
            terminal.settle()
            assert old != event_numbers(terminal), "wheel did not scroll the log viewport"
            assert toolbar == position(terminal, placeholder[:min(5, len(placeholder))]), "search field moved with the list"
            assert option_geometry == assert_log_toolbar(terminal, words), "scrolling moved the log toolbar"
            assert_no_log_summary(terminal)
            wait_log_footer(terminal, words, "logs.browsing", min_unread=1)
            preview.capture(terminal, output, "scrolled-%dx%d" % (width, height), args)
            terminal.key(b"l")
            dropdown_option(terminal, words["logs.level_all"], checked=True)
            preview.capture(terminal, output, "level-menu-%dx%d" % (width, height), args)
            terminal.key(b"\x1b")
            terminal.wait_absent("✓")
            assert option_geometry == assert_log_toolbar(terminal, words), "menu moved the toolbar"
            assert_log_toolbar_tab_order(terminal, words)
            # A wrapped entry may end on a continuation without event-.
            # Click a viewport cell to restore list focus before following.
            terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" %
                          (width - 3, height - 3, width - 3, height - 3)).encode())
            terminal.key(b"\x1b[F")
            terminal.settle()
            wait_log_footer(terminal, words, "logs.capturing")
            preview.capture(terminal, output, "following-%dx%d" % (width, height), args)
            if width == 40:
                terminal.key(b"f")
                wait_log_footer(terminal, words, "logs.display_paused", min_unread=1)
                preview.capture(terminal, output, "paused-40x16", args)
                terminal.click_text("↓ +")
                wait_log_footer(terminal, words, "logs.capturing")
        assert calls(report)["streams"] == 1 and calls(report)["canceled"] == 0, calls(report)

        # Pause freezes the visible snapshot, while a large incoming burst
        # still enters the bounded cache. Resume reveals only the newest 200.
        terminal.key(b"f")
        wait_log_action(terminal, words["logs.resume_display"])
        terminal.settle()
        assert_log_controls(terminal, words)
        frozen = event_numbers(terminal)
        assert frozen, "pause lost the visible snapshot"
        (output / "burst-events").touch()
        wait_until(terminal, lambda: calls(report).get("burst_events") == 230, "paused view stopped the background burst")
        assert_continuing(terminal, report)
        assert event_numbers(terminal) == frozen, "paused display changed after cache eviction"
        assert_stream(terminal, report, 1)
        wait_log_footer(terminal, words, "logs.display_paused", min_unread=200)
        preview.capture(terminal, output, "display-paused-cache-continues", args)
        terminal.key(b" ")
        wait_log_action(terminal, words["logs.pause_display"])
        wait_until(terminal, lambda: event_numbers(terminal) and max(event_numbers(terminal)) > max(frozen) + 200, "Space did not resume the latest cached entries")
        wait_log_footer(terminal, words, "logs.capturing")
        assert_no_log_summary(terminal)
        assert not re.search(r"\d+ / \d+", terminal.screen.text()), "unfiltered resumed cache exposed a count"
        terminal.key(b"\x1b[H")
        terminal.settle()
        assert min(event_numbers(terminal)) > max(frozen), "resumed cache retained evicted old entries"
        preview.capture(terminal, output, "resumed-cache-limit", args)
        terminal.key(b"\x1b[F")
        terminal.settle()
        terminal.click_text(words["logs.pause_display"])
        wait_log_action(terminal, words["logs.resume_display"])
        frozen = event_numbers(terminal)
        assert_continuing(terminal, report)
        assert event_numbers(terminal) == frozen, "Pause button failed to freeze display"
        terminal.key(b"\x1b[F")
        wait_log_action(terminal, words["logs.pause_display"])
        wait_log_footer(terminal, words, "logs.capturing")
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
        assert_log_controls(terminal, words)
        preview.capture(terminal, output, "clear-continuing-stream", args)
        terminal.key(b"\x12")
        wait_until(terminal, lambda: calls(report).get("streams") == 2, "Ctrl+R did not replace the log request")
        wait_log_footer(terminal, words, "logs.recovered")
        assert_no_connection_notice_in_list(terminal, words)
        assert_continuing(terminal, report)
        assert_stream(terminal, report, 2, 1)
        assert_log_controls(terminal, words)
        preview.capture(terminal, output, "manual-reconnect-once", args)

        # A single real backend error must recover without pressing Ctrl+R, and the
        # interruption belongs to status rather than an invented log record.
        (output / "break-stream").touch()
        wait_until(terminal, lambda: calls(report).get("failures") == 1, "fixture did not interrupt the active request")
        wait_log_footer(terminal, words, "logs.reconnecting")
        assert_no_connection_notice_in_list(terminal, words)
        preview.capture(terminal, output, "interrupted-retrying-status", args)
        wait_until(terminal, lambda: calls(report).get("streams") == 3, "interrupted stream did not reconnect automatically")
        wait_log_footer(terminal, words, "logs.recovered")
        assert_no_connection_notice_in_list(terminal, words)
        preview.capture(terminal, output, "automatic-recovery-status", args)
        assert_continuing(terminal, report)
        assert_stream(terminal, report, 3, 1, 1)
        assert_log_controls(terminal, words)
        wait_log_footer(terminal, words, "logs.capturing")
        assert_no_connection_notice_in_list(terminal, words)
        assert words["logs.recovered"] not in terminal.screen.text(), "restored notice did not expire"
        assert "synthetic log stream interrupted" not in terminal.screen.text(), "stale transport error survived recovery"
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
        print("PASS %s/%s: default word wrap and v toggle, conditional outside count, shared level dropdown, local case/whole-word/regex search, session capture across navigation/hidden hints/form, frozen display with 200-entry live cache, fixed toolbar at five sizes, clear/manual reconnect/automatic recovery without GAP entries; %s" %
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
    import test_artifacts

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, help="Use an existing TUI test binary (default: build a temporary one)")
    parser.add_argument("--lang", choices=["en", "zh-Hans"], action="append")
    parser.add_argument("--profile", choices=["truecolor", "256", "no-color"], action="append")
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("logs_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    test_artifacts.run_pty(parser, run_suite, preview)


def run_suite(args, parser, preview):
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor",):
            exercise(preview, args.binary, output / language / profile, language, profile, args)


if __name__ == "__main__":
    main()
