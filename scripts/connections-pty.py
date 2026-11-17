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
import unicodedata


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


TOTAL_FIELDS = ("download", "upload")
BYTE_VALUE = r"([0-9.]+ (?:B|KiB|MiB|GiB|TiB))"


def tab_label(words, view, count):
    return words["connections.tab_" + view] % str(count)


def check_tabs(terminal, words, active, closed):
    terminal.wait(tab_label(words, "active", active))
    terminal.wait(tab_label(words, "closed", closed))


def header_totals(terminal, words, required=True):
    rows = terminal.screen.text().splitlines()
    tab_prefix = words["connections.tab_active"].split("%", 1)[0]
    header_end = next((i for i, row in enumerate(rows) if tab_prefix in row), len(rows))
    header = "\n".join(rows[:header_end])
    values, points = {}, {}
    for field, arrow in (("download", "↓"), ("upload", "↑")):
        label = words["connections.summary_" + field]
        match = re.search(re.escape(label) + r":?\s*" + BYTE_VALUE, header)
        anchor = label
        if match is None:
            match = re.search(re.escape(arrow) + r"\s*" + BYTE_VALUE, header)
            anchor = arrow
        if match is None and not required:
            return None
        assert match, "missing header traffic total: " + label + "\n" + terminal.screen.text()
        values[field] = match.group(1)
        points[field] = position(terminal, anchor)
    for removed in ("Memory:", "内存占用:"):
        assert removed not in terminal.screen.text(), "removed memory summary is still visible"
    return values, points


def click_cell(terminal, x, y):
    terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())
    terminal.settle()


def resize_terminal(terminal, words, width, height):
    terminal.resize(width, height)
    # The resized emulator initially retains cells from the previous frame.
    # The pinned Quit text at its new right edge acknowledges new geometry.
    quit_label = words["dashboard.footer.quit"]
    quit_width = sum(2 if unicodedata.east_asian_width(char) in "WF" else 1 for char in quit_label)
    wait_until(terminal, lambda: "".join(terminal.screen.cells[height - 1][width - 2 - quit_width:width - 2]) == quit_label,
        "Terminal did not paint the new %dx%d footer" % (width, height))
    terminal.settle()


def search_frame(terminal, required=True):
    cells = terminal.screen.cells
    for y, row in enumerate(cells[:-2]):
        for x, value in enumerate(row):
            if value != "╭":
                continue
            right = next((i for i in range(x + 1, len(row)) if row[i] == "╮"), None)
            if right is not None and cells[y + 1][x] == cells[y + 1][right] == "│" and cells[y + 2][x] == "╰" and cells[y + 2][right] == "╯":
                return x, y, right, y + 2
    if required:
        raise AssertionError("Search frame missing\n" + terminal.screen.text())
    return None


def filter_count(terminal, required=True):
    frame = search_frame(terminal, required)
    if frame is None:
        return None
    _, top, right, _ = frame
    tail = "".join(terminal.screen.cells[top + 1][right + 1:])
    match = re.match(r"\s*(\d+(?: / \d+)?)", tail)
    if match is None:
        if required:
            raise AssertionError("Search count is not outside the input\n" + terminal.screen.text())
        return None
    return match.group(1), (right + 1 + match.start(1), top + 1)


def wait_count(terminal, total, matched=None):
    if matched is None:
        wait_until(terminal, lambda: search_frame(terminal, required=False) is not None
            and filter_count(terminal, required=False) is None,
            "Unfiltered search still shows an external count")
        return None
    expected = "%d / %d" % (matched, total)
    wait_until(terminal, lambda: (filter_count(terminal, required=False) or (None, None))[0] == expected,
        "Search count did not become " + expected)
    return filter_count(terminal)[1]


def header_pause(terminal, words, paused=False, required=True):
    tab_prefix = words["connections.tab_active"].split("%", 1)[0]
    rows = terminal.screen.text().splitlines()
    header_end = next((i for i, row in enumerate(rows) if tab_prefix in row), len(rows))
    header = "\n".join(rows[:header_end])
    label, compact = (words["connections.resume"], "▶") if paused else (words["connections.pause"], "Ⅱ")
    anchor = label if label in header else compact if compact in header else None
    if anchor is None:
        if required:
            raise AssertionError("Pause action missing from the header\n" + terminal.screen.text())
        return None
    return position(terminal, anchor)


def wait_header_pause(terminal, words, paused=False):
    wait_until(terminal, lambda: header_pause(terminal, words, paused, required=False) is not None,
        "Header pause state did not update")
    return header_pause(terminal, words, paused)


def check_filter_layout(terminal, words, active, closed, closed_view=False, paused=False):
    left, top, right, bottom = search_frame(terminal)
    assert filter_count(terminal, required=False) is None, "unfiltered search still shows an external count"
    count_point = None
    sort_label = words["connections.sort"] % words["connections.sort_newest"]
    sort_x, sort_y = position(terminal, sort_label)
    tabs = [position(terminal, tab_label(words, view, count)) for view, count in (("active", active), ("closed", closed))]
    if sort_y == top + 1:
        assert sort_x > right and all(y == top + 1 and x < left for x, y in tabs), "wide tabs/search/count/sort did not stay on one row"
    else:
        assert sort_y < top and all(y < top for _, y in tabs), "narrow tabs and sort are not above search"
    inside = "".join(terminal.screen.cells[top + 1][left + 1:right])
    for label in (words["connections.pause"], words["connections.resume"], sort_label):
        assert label not in inside, "an action is still embedded in the search field"
    pause_x, pause_y = header_pause(terminal, words, paused)
    rows = terminal.screen.text().splitlines()
    header = "\n".join(rows[:min(y for _, y in tabs)])
    close_label = words["connections.close_all"] if words["connections.close_all"] in header else "×"
    close_x, close_y = position(terminal, close_label)
    assert pause_y == close_y and pause_x < close_x and pause_y < top, "Pause is not in the header before Close all"
    if closed_view:
        terminal.wait(words["connections.history_scope"])
    elif paused:
        terminal.wait(words["connections.paused"])
    else:
        live = words["runtime.status.live"]
        assert not any(live in row for row in rows[bottom + 1:-1]), "normal live status still consumes a separate line"
    return (left, top, right, bottom), count_point, (sort_x, sort_y)


def check_sort_boundary(preview, terminal, words, output, language, args):
    width, height = (54 if language == "en" else 48), 20
    resize_terminal(terminal, words, width, height)
    terminal.key(b"\x1b[H")
    terminal.settle()
    wait_count(terminal, 48, 2)
    frame = search_frame(terminal)
    count_point = filter_count(terminal)[1]
    sort_point = position(terminal, words["connections.sort"] % words["connections.sort_newest"])
    modes = {}
    for name in ("download_rate", "upload_rate", "download_total", "upload_total", "duration", "newest"):
        terminal.key(b"s")
        label = words["connections.sort"] % words["connections.sort_" + name]
        terminal.wait(label)
        terminal.settle()
        actual_frame = search_frame(terminal)
        actual_count = filter_count(terminal)[1]
        actual_sort = position(terminal, label)
        assert actual_frame == frame, "sort label moved or resized search at the wrap breakpoint: " + name
        assert actual_count == count_point, "sort label moved the external count at the wrap breakpoint: " + name
        assert actual_sort == sort_point, "sort label moved its reserved control position at the wrap breakpoint: " + name
        modes[name] = {"frame": actual_frame, "count": actual_count, "sort": actual_sort}
        preview.capture(terminal, output, "sort-boundary-%dx%d-%s" % (width, height, name), args)
    (output / "sort-boundary.json").write_text(json.dumps({"terminal_size": [width, height], "modes": modes}, indent=2) + "\n")
    resize_terminal(terminal, words, 160, 45)


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"c")
        terminal.wait("api.github.com")
        wait_header_pause(terminal, words)
        wait_until(terminal, lambda: query_count(report) >= 2, "second runtime sample missing")
        terminal.settle()
        check_tabs(terminal, words, 48, 0)
        header_totals(terminal, words)
        wait_count(terminal, 48)
        check_filter_layout(terminal, words, 48, 0)
        preview.capture(terminal, output, "connections-wide", args)

        # View buttons and Clear retain independent mouse hit regions.
        click_cell(terminal, *position(terminal, tab_label(words, "closed", 0)))
        terminal.wait(words["connections.closed_empty"])
        click_cell(terminal, *position(terminal, tab_label(words, "active", 48)))
        terminal.wait("api.github.com")

        # The list shortcut must remain ordinary text inside the search input.
        terminal.key(b"/")
        terminal.key(b"\x15")
        paste(terminal, "no-such-")
        terminal.key(b"v")
        terminal.wait("no-such-v")
        terminal.key(b"\r")
        terminal.wait(words["connections.no_matches"])
        check_tabs(terminal, words, 48, 0)
        wait_count(terminal, 48, 0)
        _, input_top, input_right, _ = search_frame(terminal)
        click_cell(terminal, input_right - 2, input_top + 1)
        terminal.key(b"\r")
        terminal.wait("api.github.com")
        wait_count(terminal, 48)

        # Freeze the observation so any difference while searching can only
        # come from filtering, rather than the next live traffic sample.
        click_cell(terminal, *header_pause(terminal, words))
        wait_header_pause(terminal, words, paused=True)
        terminal.wait(words["connections.paused"])
        terminal.settle()
        unfiltered_totals = header_totals(terminal, words)[0]
        for query, present, absent, matches in (("github-agent", "api.github.com", "www.youtube.com", 1),
                ("streaming", "www.youtube.com", "api.github.com", 1),
                ("HK 01", "api.github.com", "www.youtube.com", 24)):
            search(terminal, query)
            terminal.wait(present)
            terminal.wait_absent(absent)
            wait_count(terminal, 48, matches)
            check_tabs(terminal, words, 48, 0)
            assert header_totals(terminal, words)[0] == unfiltered_totals, "search changed core traffic totals"
        preview.capture(terminal, output, "filtered-header-totals", args)
        terminal.key(b"p")
        wait_header_pause(terminal, words)
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
        resize_terminal(terminal, words, 80, 24)
        terminal.settle()
        terminal.key(b"p")
        wait_header_pause(terminal, words, paused=True)
        terminal.settle()
        frozen = host_rows(terminal, "api.github.com", 2)
        frozen_totals = header_totals(terminal, words)[0]
        queries_before = query_count(report)
        wait_until(terminal, lambda: query_count(report) >= queries_before + 2,
            "pause stopped background runtime monitoring", timeout=8)
        assert frozen == host_rows(terminal, "api.github.com", 2), "paused connection counters or age changed"
        check_tabs(terminal, words, 48, 0)
        assert frozen_totals == header_totals(terminal, words)[0], "paused header totals changed while runtime continued"
        preview.capture(terminal, output, "paused-snapshot", args)
        terminal.key(b"p")
        wait_header_pause(terminal, words)
        wait_until(terminal, lambda: frozen != host_rows(terminal, "api.github.com", 2),
            "resuming did not show the current connection data")
        wait_until(terminal, lambda: frozen_totals != header_totals(terminal, words)[0],
            "resuming did not show the current header totals")

        # Each sort mode remains reachable from content focus and preserves
        # the filter; six presses complete the cycle back to newest.
        resize_terminal(terminal, words, 160, 45)
        terminal.settle()
        search(terminal, "agent")
        terminal.wait("[2001:db8::42]:443")
        assert position(terminal, "api.github.com")[1] < position(terminal, "[2001:db8::42]:443")[1], "newest order is incorrect"
        for name in ("download_rate", "upload_rate", "download_total", "upload_total", "duration", "newest"):
            if name == "download_rate":
                click_cell(terminal, *position(terminal, words["connections.sort"] % words["connections.sort_newest"]))
            else:
                terminal.key(b"s")
            terminal.wait(words["connections.sort_" + name])
            terminal.settle()
            terminal.wait("api.github.com")
            terminal.wait("[2001:db8::42]:443")
            github_y = position(terminal, "api.github.com")[1]
            ipv6_y = position(terminal, "[2001:db8::42]:443")[1]
            assert (github_y < ipv6_y) == (name == "newest"), "connection sort order is incorrect: " + name
        preview.capture(terminal, output, "sort-cycle", args)
        if args.sort_boundary:
            check_sort_boundary(preview, terminal, words, output, language, args)
        search(terminal, "")

        # Scroll while toolbar cells remain fixed, then resize repeatedly from
        # a populated list. Narrow layouts retain two independently clickable rows.
        for width, height in SIZES:
            resize_terminal(terminal, words, width, height)
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait("api.github.com")
            check_tabs(terminal, words, 48, 0)
            positions = header_totals(terminal, words)[1]
            wait_count(terminal, 48)
            filter_geometry = check_filter_layout(terminal, words, 48, 0)
            if width >= 120:
                close_x, close_y = position(terminal, words["connections.close_all"])
                assert positions["download"][1] == positions["upload"][1] == close_y, "traffic totals left the header row"
                assert positions["download"][0] < positions["upload"][0] < close_x, "traffic totals are not to the left of Close all"
                if width >= 160:
                    assert positions["download"][0] > width // 2, "traffic totals are not on the right side of the header"
            tabs = {view: position(terminal, tab_label(words, view, count))
                for view, count in (("active", 48), ("closed", 0))}
            pause_position = header_pause(terminal, words)
            for _ in range(5):
                terminal.key(("\x1b[<65;%d;%dM" % (width - 3, height - 3)).encode())
            terminal.settle()
            terminal.wait_absent("api.github.com")
            assert pause_position == header_pause(terminal, words), "scroll moved the header pause action"
            assert filter_geometry == check_filter_layout(terminal, words, 48, 0), "scroll moved the fixed filter controls"
            assert positions == header_totals(terminal, words)[1], "scroll moved the header totals"
            assert tabs == {view: position(terminal, tab_label(words, view, count))
                for view, count in (("active", 48), ("closed", 0))}, "scroll moved the view tabs"
            preview.capture(terminal, output, "scrolled-%dx%d" % (width, height), args)
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait("api.github.com")
            preview.capture(terminal, output, "connections-%dx%d" % (width, height), args)

        resize_terminal(terminal, words, 80, 24)
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

        # Close one live connection from a paused observation. Its closed
        # history must stay frozen until resume, while the backend keeps polling.
        search(terminal, "github-agent")
        terminal.key(b"p")
        wait_header_pause(terminal, words, paused=True)
        terminal.key(b"x")
        terminal.wait(words["connections.confirm_title"])
        terminal.wait("api.github.com")
        preview.capture(terminal, output, "single-close-confirmation", args)
        terminal.key(b"y")
        wait_until(terminal, lambda: words["connections.inactive_detail"] in terminal.screen.text()
            or words["connections.archived"] in terminal.screen.text(), "closed connection detail did not update")
        terminal.key(b"\x1b")
        terminal.wait_absent(words["connections.details"])
        search(terminal, "")
        check_tabs(terminal, words, 48, 0)
        terminal.key(b"v")
        terminal.wait(words["connections.closed_empty"])
        queries_before = query_count(report)
        wait_until(terminal, lambda: query_count(report) >= queries_before + 2,
            "paused archive stopped background monitoring", timeout=8)
        check_tabs(terminal, words, 48, 0)
        terminal.wait_absent("api.github.com")
        preview.capture(terminal, output, "paused-closed-history", args)
        terminal.key(b"p")
        wait_header_pause(terminal, words)
        check_tabs(terminal, words, 47, 1)
        terminal.wait("api.github.com")
        search(terminal, "github-agent")
        wait_count(terminal, 1, 1)
        preview.capture(terminal, output, "closed-single-filtered", args)
        terminal.key(b"\r")
        terminal.wait(words["connections.details"])
        terminal.wait(words["connections.archived"])
        terminal.wait("api.github.com")
        terminal.wait_absent(words["connections.close_shortcut"])
        terminal.key(b"x")
        terminal.settle()
        terminal.wait_absent(words["connections.confirm_title"])
        terminal.wait(words["connections.archived"])
        preview.capture(terminal, output, "closed-single-details", args)
        terminal.key(b"\x1b")
        terminal.wait_absent(words["connections.details"])
        terminal.key(b"v")
        terminal.wait(words["connections.no_matches"])
        search(terminal, "")
        check_tabs(terminal, words, 47, 1)
        terminal.wait("www.youtube.com")
        terminal.wait_absent("api.github.com")
        preview.capture(terminal, output, "active-after-single-close", args)

        # The header action always closes the live core's entire connection
        # list, including rows hidden by this filter. Enter first exercises
        # the default Cancel action before explicitly confirming a new attempt.
        resize_terminal(terminal, words, 160, 45)
        terminal.settle()
        search(terminal, "streaming")
        terminal.wait("www.youtube.com")
        terminal.wait_absent("flow-04.example.invalid")
        terminal.click_text(words["connections.close_all"])
        terminal.wait(words["connections.confirm_all_title"])
        preview.capture(terminal, output, "close-all-default-cancel", args)
        terminal.key(b"\r")
        terminal.wait_absent(words["connections.confirm_all_title"])
        terminal.wait("www.youtube.com")
        action_hint = words["connections.close_all_hint"].split(" · ")[0]
        terminal.wait(action_hint)
        terminal.settle()
        footer = terminal.screen.text().splitlines()[-1]
        assert action_hint in footer and words["dashboard.footer.help"] in footer, "header focus lost its action hint or pinned help"
        assert words["header.refresh"] not in footer, "close-all header still advertises refresh"
        preview.capture(terminal, output, "focused-close-all", args)
        terminal.click_text(words["connections.close_all"])
        terminal.wait(words["connections.confirm_all_title"])
        preview.capture(terminal, output, "close-all-confirmation", args)
        terminal.key(b"y")
        terminal.wait(words["connections.no_matches"])
        assert words["connections.close_all_requested"] not in terminal.screen.text(), "routine success unexpectedly opened a toast"
        search(terminal, "")
        terminal.wait(words["connections.empty"])
        terminal.wait_absent("www.youtube.com")
        check_tabs(terminal, words, 0, 48)
        # Routine success stays quiet; retain cumulative totals after the close.
        wait_until(terminal, lambda: header_totals(terminal, words, required=False) is not None,
            "header totals stayed obscured after closing all connections", timeout=12)
        values = header_totals(terminal, words)[0]
        assert values["download"].endswith("GiB") and values["upload"].endswith("MiB"), "close-all cleared cumulative traffic"
        preview.capture(terminal, output, "close-all-empty", args)

        # All 48 observed connections remain searchable in this session.
        terminal.key(b"v")
        wait_count(terminal, 48)
        search(terminal, "github-agent")
        terminal.wait("api.github.com")
        wait_count(terminal, 48, 1)
        preview.capture(terminal, output, "closed-all-filtered", args)
        search(terminal, "flow-")
        wait_count(terminal, 48, 44)
        search(terminal, "")
        for width, height in SIZES:
            resize_terminal(terminal, words, width, height)
            terminal.key(b"\x1b[H")
            terminal.settle()
            check_tabs(terminal, words, 0, 48)
            wait_count(terminal, 48)
            header_totals(terminal, words)
            check_filter_layout(terminal, words, 0, 48, closed_view=True)
            if width >= 160:
                terminal.wait(words["navigation.connections.action.details"])
                terminal.settle()
            footer = terminal.screen.text().splitlines()[-1]
            if width >= 160:
                for hint_key in ("navigation.connections.action.details", "navigation.connections.search", "navigation.connections.active", "navigation.connections.browse", "navigation.connections.sort", "dashboard.footer.help"):
                    assert words[hint_key] in footer, "closed list lost its available action hint: " + hint_key
            assert not re.search(r"(?:^| · )x\s", footer), "closed list still advertises closing connections"
            preview.capture(terminal, output, "closed-%dx%d" % (width, height), args)
        terminal.key(b"v")
        terminal.wait(words["connections.empty"])
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
        print("PASS %s/%s: search, rate samples, live pause, six sorts, details/two-row clicks, fixed toolbar, six sizes, header pause/totals, framed search with outside count/sort, active/closed tabs and frozen history, exact close, archived filters/details, close-all cancellation/confirmation; %s" %
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
    parser.add_argument("--sort-boundary", action="store_true", help="Check all sort labels at the English/Chinese wrap breakpoint")
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
