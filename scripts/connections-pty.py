#!/usr/bin/env python3
"""Verify Connections using a real controlling PTY and an opt-in memory backend.

Build with `go test -c ./internal/tui -o bin/connections-tui.test`. This fixture
never contacts a core, network or system service. Optional PNGs redraw captured
VT cells and colors, rather than photographing a native terminal application.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys
import time
import uuid


SIZES = ((200, 45), (160, 45), (120, 32), (80, 24), (60, 20), (40, 16), (160, 45))


def labels(language):
    result = {}
    directory = Path(__file__).resolve().parents[1] / "internal" / "i18n" / "locales"
    for path in directory.glob(language + ".*.json"):
        result.update(json.loads(path.read_text()))
    return result


def wait_until(terminal, predicate, label, timeout=10):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        terminal.read(0.05)
        if not terminal.screen.synchronized and terminal.screen.state == "text" and predicate():
            return
        if terminal.closed:
            break
    raise AssertionError(label + "\n" + terminal.screen.text())


def position(terminal, label):
    for y, row in enumerate(terminal.screen.cells):
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(label):
                return x, y
    raise AssertionError("Visible text missing: " + label + "\n" + terminal.screen.text())


def paste(terminal, value):
    terminal.key(b"\x1b[200~" + value.encode() + b"\x1b[201~")


def search(terminal, value):
    terminal.key(b"/")
    terminal.key(b"\x15")
    paste(terminal, value)
    terminal.key(b"\r")
    terminal.settle()


def fixture_terminal(preview, binary, report, language, profile):
    environment = {"LAZYCLASH_CONNECTIONS_PTY_REPORT": str(report),
                   "LAZYCLASH_CONNECTIONS_PTY_LANGUAGE": language}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestConnectionsPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def query_count(report):
    try:
        return int(Path(str(report) + ".runtime").read_text())
    except (FileNotFoundError, ValueError):
        return 0


def host_rows(terminal, host, count=1):
    rows = terminal.screen.text().splitlines()
    y = position(terminal, host)[1]
    return rows[y:y + count]


SUMMARY_FIELDS = ("download", "upload", "memory", "active")


def summary_values(terminal, words, active=None):
    screen = terminal.screen.text()
    values = {}
    for field in SUMMARY_FIELDS:
        label = words["connections.summary_" + field]
        match = re.search(re.escape(label) + r":\s*([0-9.]+(?: (?:B|KiB|MiB|GiB|TiB))?)", screen)
        assert match, "missing connection summary field: " + label + "\n" + screen
        values[field] = match.group(1)
    assert values["memory"] == "32.0 MiB", "summary used standalone runtime memory: " + repr(values)
    if active is not None:
        assert values["active"] == str(active), "summary active count follows filter or lost live data: " + repr(values)
    return values


def summary_positions(terminal, words):
    return {field: position(terminal, words["connections.summary_" + field]) for field in SUMMARY_FIELDS}


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"c")
        terminal.wait("api.github.com")
        terminal.wait(words["connections.pause"])
        wait_until(terminal, lambda: query_count(report) >= 2, "second runtime sample missing")
        terminal.settle()
        summary_values(terminal, words, active=48)
        preview.capture(terminal, output, "connections-wide", args)

        # Freeze the observation so any difference while searching can only
        # come from filtering, rather than the next live traffic sample.
        terminal.key(b"p")
        terminal.wait(words["connections.resume"])
        terminal.settle()
        unfiltered_summary = summary_values(terminal, words, active=48)
        for query, present, absent, matches in (("github-agent", "api.github.com", "www.youtube.com", 1),
                ("streaming", "www.youtube.com", "api.github.com", 1),
                ("HK 01", "api.github.com", "www.youtube.com", 24)):
            search(terminal, query)
            terminal.wait(present)
            terminal.wait_absent(absent)
            terminal.wait(words["connections.count"] % (matches, 48))
            assert summary_values(terminal, words, active=48) == unfiltered_summary, "search changed core summary values"
        preview.capture(terminal, output, "filtered-summary", args)
        terminal.key(b"p")
        terminal.wait(words["connections.pause"])
        search(terminal, "github-agent")
        terminal.key(b"\r")
        terminal.wait(words["connections.details"])
        terminal.wait("pty-connection-00")
        terminal.wait("/fixture/github-agent")
        preview.capture(terminal, output, "connection-details", args)
        terminal.key(b"\x1b")
        terminal.wait_absent(words["connections.details"])
        terminal.wait("api.github.com")
        assert "github-agent" in terminal.screen.text(), "detail return lost the filter"

        # A paused connection list freezes its data while runtime monitoring
        # continues in the background, including sidebar metrics.
        wait_until(terminal, lambda: query_count(report) >= 2, "second runtime sample missing")
        terminal.resize(80, 24)
        terminal.settle()
        terminal.key(b"p")
        terminal.wait(words["connections.resume"])
        terminal.settle()
        frozen = host_rows(terminal, "api.github.com", 2)
        frozen_summary = summary_values(terminal, words, active=48)
        queries_before = query_count(report)
        wait_until(terminal, lambda: query_count(report) >= queries_before + 2,
            "pause stopped background runtime monitoring", timeout=8)
        assert frozen == host_rows(terminal, "api.github.com", 2), "paused connection counters or age changed"
        assert frozen_summary == summary_values(terminal, words, active=48), "paused summary changed while runtime continued"
        preview.capture(terminal, output, "paused-snapshot", args)
        terminal.key(b"p")
        terminal.wait(words["connections.pause"])
        wait_until(terminal, lambda: frozen != host_rows(terminal, "api.github.com", 2),
            "resuming did not show the current connection data")
        wait_until(terminal, lambda: frozen_summary != summary_values(terminal, words, active=48),
            "resuming did not show the current summary totals")

        # Each sort mode remains reachable from content focus and preserves
        # the filter; six presses complete the cycle back to newest.
        terminal.resize(160, 45)
        terminal.settle()
        search(terminal, "agent")
        terminal.wait("[2001:db8::42]:443")
        assert position(terminal, "api.github.com")[1] < position(terminal, "[2001:db8::42]:443")[1], "newest order is incorrect"
        for name in ("download_rate", "upload_rate", "download_total", "upload_total", "duration", "newest"):
            terminal.key(b"s")
            terminal.wait(words["connections.sort_" + name])
            terminal.settle()
            terminal.wait("api.github.com")
            terminal.wait("[2001:db8::42]:443")
            github_y = position(terminal, "api.github.com")[1]
            ipv6_y = position(terminal, "[2001:db8::42]:443")[1]
            assert (github_y < ipv6_y) == (name == "newest"), "connection sort order is incorrect: " + name
        preview.capture(terminal, output, "sort-cycle", args)
        search(terminal, "")

        # Scroll while toolbar cells remain fixed, then resize repeatedly from
        # a populated list. Narrow layouts retain two independently clickable rows.
        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait("api.github.com")
            summary_values(terminal, words, active=48)
            positions = summary_positions(terminal, words)
            expected_rows = 1 if width >= 160 or (width == 120 and language == "zh-Hans") else 2 if width >= 60 else 4
            assert len({point[1] for point in positions.values()}) == expected_rows, "summary did not adapt its columns to terminal width"
            pause_position = position(terminal, words["connections.pause"])
            for _ in range(5):
                terminal.key(("\x1b[<65;%d;%dM" % (width - 3, height - 3)).encode())
            terminal.settle()
            terminal.wait_absent("api.github.com")
            assert pause_position == position(terminal, words["connections.pause"]), "scroll moved the fixed toolbar"
            assert positions == summary_positions(terminal, words), "scroll moved the fixed summary"
            summary_values(terminal, words, active=48)
            preview.capture(terminal, output, "scrolled-%dx%d" % (width, height), args)
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait("api.github.com")
            preview.capture(terminal, output, "connections-%dx%d" % (width, height), args)

        terminal.resize(80, 24)
        search(terminal, "github-agent")
        x, y = position(terminal, "api.github.com")
        terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 2, x + 1, y + 2)).encode())
        terminal.wait(words["connections.details"])
        terminal.wait("api.github.com")
        terminal.wait("github-agent")
        terminal.key(b"\x1b")
        terminal.wait_absent(words["connections.details"])

        search(terminal, "ipv6-agent")
        terminal.key(b"\r")
        terminal.wait(words["connections.details"])
        terminal.wait("[2001:db8::42]:443")
        preview.capture(terminal, output, "ipv6-details", args)
        terminal.key(b"\x1b")
        terminal.wait_absent(words["connections.details"])
        terminal.settle()
        search(terminal, "no-such-connection-pty")
        terminal.wait(words["connections.no_matches"])
        preview.capture(terminal, output, "no-matches", args)

        # Close only the reviewed live ID. The filtered list empties, while
        # another connection remains available after clearing the search.
        search(terminal, "github-agent")
        terminal.key(b"x")
        terminal.wait(words["connections.confirm_title"])
        terminal.wait("api.github.com")
        preview.capture(terminal, output, "single-close-confirmation", args)
        terminal.key(b"y")
        terminal.wait(words["connections.inactive_detail"])
        terminal.key(b"\x1b")
        terminal.wait(words["connections.no_matches"])
        search(terminal, "")
        terminal.wait("www.youtube.com")
        terminal.wait_absent("api.github.com")

        # The header action always closes the live core's entire connection
        # list, including rows hidden by this filter. Enter first exercises
        # the default Cancel action before explicitly confirming a new attempt.
        search(terminal, "streaming")
        terminal.wait("www.youtube.com")
        terminal.wait_absent("flow-04.example.invalid")
        terminal.click_text(words["connections.close_all"])
        terminal.wait(words["connections.confirm_all_title"])
        preview.capture(terminal, output, "close-all-default-cancel", args)
        terminal.key(b"\r")
        terminal.wait_absent(words["connections.confirm_all_title"])
        terminal.wait("www.youtube.com")
        terminal.wait(words["connections.close_all_hint"])
        terminal.settle()
        footer = terminal.screen.text().splitlines()[-1]
        assert words["connections.close_all_hint"] in footer, "header focus lost the close-all help text"
        assert words["header.refresh"] not in footer, "close-all header still advertises refresh"
        preview.capture(terminal, output, "focused-close-all", args)
        terminal.click_text(words["connections.close_all"])
        terminal.wait(words["connections.confirm_all_title"])
        preview.capture(terminal, output, "close-all-confirmation", args)
        terminal.key(b"y")
        terminal.wait(words["connections.close_all_requested"])
        terminal.wait(words["connections.no_matches"])
        search(terminal, "")
        terminal.wait(words["connections.empty"])
        terminal.wait_absent("www.youtube.com")
        # Success toasts can temporarily cover the first summary row. Inspect
        # the underlying counters once both operation notifications expire.
        wait_until(terminal, lambda: all(words["connections.summary_" + field] + ":" in terminal.screen.text()
            for field in SUMMARY_FIELDS), "summary stayed obscured after closing all connections", timeout=12)
        values = summary_values(terminal, words, active=0)
        assert values["download"].endswith("GiB") and values["upload"].endswith("MiB"), "close-all cleared cumulative traffic"
        preview.capture(terminal, output, "close-all-empty", args)
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["runtime_queries"] >= 4, calls
        assert calls["closes"] == 1 and calls["closed_ids"] == ["pty-connection-00"], calls
        assert calls["close_all"] == 1 and calls["connections"] == 0, calls
        assert calls["service_changes"] == calls["modes"] == calls["selections"] == 0, calls
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "application changed the terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: search, rate samples, live pause, six sorts, details/two-row clicks, fixed toolbar, six sizes, four fixed summary fields, exact close, filtered close-all cancellation/confirmation; %s" %
            (language, profile, json.dumps(calls)), flush=True)
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
    spec = importlib.util.spec_from_file_location("connections_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
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
