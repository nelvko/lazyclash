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


def cell_colors(terminal, point):
    x, y = point
    return terminal.screen.styles[y][x][:2]


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


def matching_options(terminal):
    left, top, right, bottom = search_frame(terminal)
    found = {}
    for label in ("Aa", "ab", ".*"):
        for y in (top + 1, bottom + 1):
            if y >= len(terminal.screen.cells):
                continue
            row = terminal.screen.cells[y]
            for x in range(left + 1, right):
                if "".join(row[x:]).startswith(label):
                    found[label] = (x, y)
                    break
            if label in found:
                break
        assert label in found, "search matching option missing: " + label
    assert list(found.values()) == sorted(found.values(), key=lambda point: (point[1], point[0])), \
        "search matching options do not read Aa, ab, .*"
    return found


def clear_search_position(terminal):
    left, top, right, _ = search_frame(terminal)
    row = terminal.screen.cells[top + 1]
    candidates = [x for x in range(left + 1, right) if row[x] == "x"
                  and row[x - 1] in (" ", "›") and row[x + 1] in (" ", "│")]
    assert candidates, "search Clear control missing"
    return candidates[-1], top + 1


def filter_count(terminal, required=True):
    frame = search_frame(terminal, required)
    if frame is None:
        return None
    _, top, right, bottom = frame
    # The ratio follows the input, or moves below the matching controls.
    candidates = [(top + 1, right + 1, False)]
    candidates += [(y, 0, True) for y in range(bottom + 1, min(bottom + 4, len(terminal.screen.cells)))]
    for y, start, separate in candidates:
        text = "".join(terminal.screen.cells[y][start:])
        pattern = r"^\s*(\d+(?: / \d+)?)\s*$" if separate else r"(?<!\w)(\d+(?: / \d+)?)(?!\w)"
        match = re.search(pattern, text)
        if match is not None:
            prefix = text[:match.start(1)]
            x = start + sum(2 if unicodedata.east_asian_width(char) in "WF" else 1 for char in prefix)
            return match.group(1), (x, y)
    if required:
        raise AssertionError("Search count is not outside the input\n" + terminal.screen.text())
    return None


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


def table_header_y(terminal, words):
    for y, row in enumerate(terminal.screen.cells):
        text = "".join(row)
        if words["connections.destination"] in text and words["connections.chain"] in text:
            return y
    return None


def sort_position(terminal, words):
    y = table_header_y(terminal, words)
    if y is None:
        return position(terminal, words["connections.sort"].split("%s")[0])
    return position(terminal, words["connections.destination"])


def click_header(terminal, words, field):
    y = table_header_y(terminal, words)
    row = terminal.screen.cells[y]
    label = words["connections." + field]
    x = next(i for i, cell in enumerate(row) if cell and "".join(row[i:]).startswith(label))
    click_cell(terminal, x, y)


def header_bulk_action(terminal, words, closed_view, paused=False):
    _, y = header_pause(terminal, words, paused)
    label, compact = (words["connections.clear_history"], "⌫") if closed_view else (words["connections.close_all"], "×")
    row = terminal.screen.cells[y]
    visible = label if label in "".join(row) else compact
    for x, cell in enumerate(row):
        if cell and "".join(row[x:]).startswith(visible):
            return x, y
    raise AssertionError("Missing header action: " + label + "\n" + terminal.screen.text())


def check_filter_layout(terminal, words, active, closed, closed_view=False, paused=False):
    left, top, right, bottom = search_frame(terminal)
    assert filter_count(terminal, required=False) is None, "unfiltered search still shows an external count"
    sort_label = words["connections.sort"].split("%s")[0]
    sort_x, sort_y = sort_position(terminal, words)
    tabs = [position(terminal, tab_label(words, view, count)) for view, count in (("active", active), ("closed", closed))]
    if table_header_y(terminal, words) is not None:
        assert sort_y > bottom and sort_label not in terminal.screen.text(), "wide table still has a separate sort control"
        assert all(y == top + 1 and x < left for x, y in tabs), "tabs are not next to the search field"
    else:
        assert sort_y < top and all(y < top for _, y in tabs), "narrow tabs and sort are not above search"
    options = matching_options(terminal)
    for x, y in options.values():
        assert (y == top + 1 and left < x < right) or y == bottom + 1, \
            "matching controls escaped their input component"
    inside = "".join(terminal.screen.cells[top + 1][left + 1:right])
    for label in (words["connections.pause"], words["connections.resume"], sort_label):
        assert label not in inside, "an action is still embedded in the search field"
    pause_x, pause_y = header_pause(terminal, words, paused)
    rows = terminal.screen.text().splitlines()
    bulk_x, bulk_y = header_bulk_action(terminal, words, closed_view, paused)
    assert pause_y == bulk_y and pause_x < bulk_x and pause_y < top, "bulk action should follow Pause"
    if paused:
        terminal.wait(words["connections.paused"])
    elif closed_view and table_header_y(terminal, words) is not None:
        assert sort_y == bottom + 1, "extra history scope row between search and table"
    else:
        live = words["runtime.status.live"]
        assert not any(live in row for row in rows[bottom + 1:-1]), "normal live status still consumes a separate line"
    return (left, top, right, bottom), (sort_x, sort_y)


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
    for key in ("sort_download_rate", "sort_upload_rate", "sort_download_total", "sort_upload_total", "sort_duration",
                "destination", "protocol", "process", "chain", "rule", "sort_newest"):
        terminal.key(b"s")
        label = words["connections.sort"] % words["connections." + key]
        terminal.wait(label)
        terminal.settle()
        actual_frame = search_frame(terminal)
        actual_count = filter_count(terminal)[1]
        actual_sort = position(terminal, label)
        assert actual_frame == frame, "sort label moved or resized search at the wrap breakpoint: " + key
        assert actual_count == count_point, "sort label moved the external count at the wrap breakpoint: " + key
        assert actual_sort == sort_point, "sort label moved its reserved control position at the wrap breakpoint: " + key
        modes[key] = {"frame": actual_frame, "count": actual_count, "sort": actual_sort}
        preview.capture(terminal, output, "sort-boundary-%dx%d-%s" % (width, height, key), args)
    (output / "sort-boundary.json").write_text(json.dumps({"terminal_size": [width, height], "modes": modes}, indent=2) + "\n")
    resize_terminal(terminal, words, 160, 45)


def exercise_search_modes(preview, terminal, words, output, args):
    search(terminal, "API.GITHUB.COM")
    wait_count(terminal, 48, 1)
    click_cell(terminal, *matching_options(terminal)["Aa"])
    wait_count(terminal, 48, 0)
    terminal.wait(words["connections.no_matches"])
    preview.capture(terminal, output, "search-match-case", args)
    terminal.key(b"\x1bc")
    wait_count(terminal, 48, 1)
    # Modified shortcuts work while the Bubbles editor owns ordinary keys.
    terminal.key(b"/")
    terminal.key(b"\x1bc")
    wait_count(terminal, 48, 0)
    terminal.key(b"\x1bc")
    wait_count(terminal, 48, 1)
    terminal.key(b"\r")

    search(terminal, "flo")
    wait_count(terminal, 48, 44)
    terminal.key(b"\x1bw")
    wait_count(terminal, 48, 0)
    search(terminal, "flow")
    wait_count(terminal, 48, 44)
    search(terminal, "中")
    wait_count(terminal, 48, 0)
    terminal.key(b"\x1bw")
    wait_count(terminal, 48, 1)
    search(terminal, "中文")
    terminal.key(b"\x1bw")
    wait_count(terminal, 48, 1)
    preview.capture(terminal, output, "search-unicode-whole-word", args)
    terminal.key(b"\x1bw")

    search(terminal, r"^api\.github\.com$")
    wait_count(terminal, 48, 0)
    terminal.key(b"\x1br")
    wait_count(terminal, 48, 1)
    for query, count in ((r"^(api\.github\.com|www\.youtube\.com)$", 2),
                         ("^github-agent$", 1), ("^HK 01$", 24), ("01 PROXY$", 0)):
        search(terminal, query)
        wait_count(terminal, 48, count)
    search(terminal, "[")
    wait_count(terminal, 48, 0)
    terminal.wait(words["connections.invalid_regex"])
    preview.capture(terminal, output, "search-invalid-regex", args)
    search(terminal, r"^api\.github\.com$")
    terminal.wait_absent(words["connections.invalid_regex"])
    wait_count(terminal, 48, 1)
    preview.capture(terminal, output, "search-corrected-regex", args)
    terminal.key(b"\x1br")
    wait_count(terminal, 48, 0)
    search(terminal, "")
    wait_count(terminal, 48)


def assert_search_tab_order(preview, terminal, words, output, width, height, args):
    frame = search_frame(terminal)
    sort_point = sort_position(terminal, words)
    search(terminal, "API.GITHUB.COM")
    count_point = wait_count(terminal, 48, 1)
    assert (search_frame(terminal), sort_position(terminal, words)) == (frame, sort_point), "typing moved the frame or sorting"
    assert (count_point[1] == frame[1] + 1 and count_point[0] > frame[2]) or count_point[1] > frame[3], "ratio is not beside or after the search component"
    terminal.key(b"/")
    terminal.key(b"\t")
    terminal.settle()
    clear_x, clear_y = clear_search_position(terminal)
    wait_until(terminal, lambda: terminal.screen.cells[clear_y][clear_x - 1] == "›",
               "Tab did not move from search to Clear")
    terminal.key(b"\t")
    terminal.settle()
    case_x, case_y = matching_options(terminal)["Aa"]
    wait_until(terminal, lambda: terminal.screen.cells[case_y][case_x - 1] == "›",
               "Tab did not move from Clear to Aa")
    terminal.key(b"\r")
    wait_count(terminal, 48, 0)
    preview.capture(terminal, output, "search-tab-%dx%d" % (width, height), args)
    terminal.key(b"\x1b[Z")
    terminal.key(b"\x1b[Z")
    terminal.key(b"\r")
    terminal.key(b"\x15")
    paste(terminal, "api.github.com")
    wait_count(terminal, 48, 1)
    terminal.key(b"\r")
    terminal.key(b"\x1bc")
    search(terminal, "")
    wait_count(terminal, 48)
    assert (search_frame(terminal), sort_position(terminal, words)) == (frame, sort_point), "clearing or matching modes moved the frame or sorting"


def exercise_table_sort(preview, terminal, words, output, args):
    resize_terminal(terminal, words, 200, 45)
    search(terminal, "agent")
    wait_count(terminal, 48, 2)
    y = table_header_y(terminal, words)
    assert y is not None and words["connections.sort"].split("%s")[0] not in terminal.screen.text()

    def check_order(label, direction, github_first):
        terminal.settle()
        heading = "".join(terminal.screen.cells[y])
        assert re.search(re.escape(label) + r"\s*" + direction, heading), "missing sort indicator: " + heading
        assert heading.count("▲") + heading.count("▼") == 1, "multiple sorted headers: " + heading
        assert (position(terminal, "api.github.com")[1] < position(terminal, "[2001:db8::42]:443")[1]) == github_first, "wrong row order: " + heading

    chain = words["connections.chain"]
    click_header(terminal, words, "chain")
    check_order(chain, "▲", False)  # DIRECT before PROXY → HK 01
    click_header(terminal, words, "chain")
    check_order(chain, "▼", True)
    preview.capture(terminal, output, "header-chain-descending", args)
    terminal.key(b"\r")
    terminal.settle()
    assert not any(arrow in "".join(terminal.screen.cells[y]) for arrow in "▲▼"), "third activation did not clear the column sort"
    assert position(terminal, "api.github.com")[1] < position(terminal, "[2001:db8::42]:443")[1], "third activation did not restore newest first"
    click_header(terminal, words, "chain")
    check_order(chain, "▲", False)
    search(terminal, "")
    terminal.key(b"\x1b[H")
    terminal.settle()
    preview.capture(terminal, output, "header-chain-ascending", args)
    search(terminal, "agent")

    for field in ("download_speed", "upload_speed", "download_total", "upload_total", "duration"):
        label = words["connections." + field]
        click_header(terminal, words, field)
        check_order(label, "▼", False)
        click_header(terminal, words, field)
        check_order(label, "▲", True)

    # Tab from Chain to its right-hand neighbor Rule, then sort using Enter.
    click_header(terminal, words, "chain")
    terminal.key(b"\t")
    terminal.key(b"\r")
    check_order(words["connections.rule"], "▲", False)

    # Switching back to the initial newest order leaves no field marked.
    for _ in range(12):
        terminal.key(b"s")
        terminal.settle()
        if not any(mark in "".join(terminal.screen.cells[y]) for mark in "▲▼"):
            break
    else:
        raise AssertionError("Keyboard sorting did not return to newest")
    assert position(terminal, "api.github.com")[1] < position(terminal, "[2001:db8::42]:443")[1]

    resize_terminal(terminal, words, 80, 24)
    newest = words["connections.sort"] % words["connections.sort_newest"]
    terminal.wait(newest)
    compact_point = position(terminal, newest)
    terminal.key(b"S")
    oldest = words["connections.sort"] % words["connections.sort_oldest"]
    terminal.wait(oldest)
    terminal.settle()
    assert position(terminal, oldest) == compact_point
    assert position(terminal, "[2001:db8::42]:443")[1] < position(terminal, "api.github.com")[1], "compact reverse did not sort oldest first"
    preview.capture(terminal, output, "compact-sort-reversed", args)
    terminal.key(b"S")
    terminal.settle()


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("Connections preview")
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
        exercise_search_modes(preview, terminal, words, output, args)

        # The list shortcut must remain ordinary text inside the search input.
        empty_geometry = search_frame(terminal), sort_position(terminal, words)
        terminal.key(b"/")
        terminal.key(b"\x15")
        paste(terminal, "no-such-")
        terminal.key(b"v")
        terminal.wait("no-such-v")
        terminal.key(b"\r")
        terminal.wait(words["connections.no_matches"])
        check_tabs(terminal, words, 48, 0)
        wait_count(terminal, 48, 0)
        assert (search_frame(terminal), sort_position(terminal, words)) == empty_geometry, "typing changed toolbar geometry"
        click_cell(terminal, *clear_search_position(terminal))
        terminal.key(b"\r")
        terminal.wait("api.github.com")
        wait_count(terminal, 48)
        assert (search_frame(terminal), sort_position(terminal, words)) == empty_geometry, "Clear changed toolbar geometry"

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
        search(terminal, "flow-")
        wait_count(terminal, 48, 44)
        host = "flow-07.example.invalid"
        row = position(terminal, host)
        neighbor = position(terminal, "flow-08.example.invalid")
        list_colors = cell_colors(terminal, neighbor)
        click_cell(terminal, *row)
        terminal.wait(words["connections.details"])
        terminal.wait("pty-connection-07")
        terminal.wait("/fixture/worker")
        title_x, title_y = position(terminal, words["connections.details"])
        assert title_x >= len(terminal.screen.cells[0]) // 2, "wide details should open on the right"
        assert re.search(r"›\s*×", "".join(terminal.screen.cells[title_y][title_x:])), "new inspector did not focus its dismiss button"
        assert "".join(terminal.screen.cells[row[1]][row[0]:]).startswith(host), "inspector displaced the selected row"
        assert neighbor == position(terminal, "flow-08.example.invalid"), "inspector displaced the background list"
        assert "▌" in terminal.screen.cells[row[1]][:row[0]], "inspector lost the selected row marker"
        if profile != "no-color":
            assert cell_colors(terminal, neighbor) == list_colors, "inspector dimmed the surrounding list"
            assert cell_colors(terminal, row)[1] != list_colors[1], "inspector lost the selected row background"
        # Review shortcuts inside an open inspector must never disconnect.
        terminal.key(b"x")
        terminal.key(b"y")
        observations = query_count(report)
        wait_until(terminal, lambda: query_count(report) >= observations + 2, "missing samples after review shortcuts")
        check_tabs(terminal, words, 48, 0)
        terminal.wait(words["connections.close"])
        terminal.wait("pty-connection-07")
        preview.capture(terminal, output, "connection-details", args)
        click_cell(terminal, *neighbor)
        terminal.wait_absent(words["connections.details"])
        wait_count(terminal, 48, 44)
        terminal.key(b"\r")
        terminal.wait("pty-connection-07")
        terminal.key(b"\x1b")
        terminal.wait_absent(words["connections.details"])
        assert position(terminal, host) == row, "dismissing the inspector moved the list"
        search(terminal, "github-agent")

        # The paused list keeps its snapshot while runtime monitoring continues.
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

        exercise_table_sort(preview, terminal, words, output, args)
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
            assert_search_tab_order(preview, terminal, words, output, width, height, args)

        resize_terminal(terminal, words, 80, 24)
        search(terminal, "github-agent")
        x, y = position(terminal, "api.github.com")
        terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 2, x + 1, y + 2)).encode())
        terminal.wait(words["connections.details"])
        terminal.wait("api.github.com")
        terminal.wait("github-agent")
        terminal.resize(40, 16)
        terminal.settle()
        terminal.wait(words["connections.details"])
        terminal.key(b"\x1b[F")
        terminal.settle()
        # Value columns can wrap at 40 cells. Both bottom fields must be reachable.
        terminal.wait(words["connections.id"])
        terminal.wait("/fixture/")
        preview.capture(terminal, output, "connection-details-40x16", args)
        terminal.key(b"\x1b")
        terminal.wait_absent(words["connections.details"])
        wait_count(terminal, 48, 1)
        resize_terminal(terminal, words, 80, 24)

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
        terminal.wait(words["connections.details"])
        terminal.wait("api.github.com")
        terminal.wait(words["connections.close"])
        preview.capture(terminal, output, "review-single-connection", args)
        terminal.click_text(words["connections.close"])
        wait_until(terminal, lambda: words["connections.inactive_detail"] in terminal.screen.text()
            or words["connections.archived"] in terminal.screen.text(), "closed connection detail did not update")
        terminal.wait_absent(words["connections.close"])
        preview.capture(terminal, output, "direct-close-result", args)
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
        terminal.wait_absent(words["connections.close"])
        terminal.key(b"x")
        terminal.key(b"y")
        terminal.settle()
        terminal.wait_absent(words["connections.close"])
        terminal.wait(words["connections.archived"])
        preview.capture(terminal, output, "closed-single-details", args)
        terminal.key(b"\x1b")
        terminal.wait_absent(words["connections.details"])
        # Clearing a single history entry leaves the other 47 connections active.
        click_cell(terminal, *header_bulk_action(terminal, words, closed_view=True))
        wait_count(terminal, 0, 0)
        check_tabs(terminal, words, 47, 0)
        terminal.key(b"v")
        terminal.wait(words["connections.no_matches"])
        search(terminal, "")
        check_tabs(terminal, words, 47, 0)
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
        row = position(terminal, "www.youtube.com")
        row_colors = cell_colors(terminal, row)
        terminal.click_text(words["connections.close_all"])
        terminal.wait(words["connections.confirm_all_title"])
        assert position(terminal, "www.youtube.com") == row, "bulk close displaced the filtered list"
        if profile != "no-color":
            header = position(terminal, words["connections.summary_download"])
            backdrop = cell_colors(terminal, row)
            assert backdrop != row_colors, "bulk close left the selected row undimmed"
            assert cell_colors(terminal, header) == backdrop, "bulk close backdrop did not cover the page header"
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
        check_tabs(terminal, words, 0, 47)
        # Routine success stays quiet; retain cumulative totals after the close.
        wait_until(terminal, lambda: header_totals(terminal, words, required=False) is not None,
            "header totals stayed obscured after closing all connections", timeout=12)
        values = header_totals(terminal, words)[0]
        assert values["download"].endswith("GiB") and values["upload"].endswith("MiB"), "close-all cleared cumulative traffic"
        preview.capture(terminal, output, "close-all-empty", args)

        # Later closures are recorded; the cleared entry stays gone.
        terminal.key(b"v")
        wait_count(terminal, 47)
        search(terminal, "github-agent")
        wait_count(terminal, 47, 0)
        search(terminal, "streaming")
        wait_count(terminal, 47, 1)
        terminal.wait("www.youtube.com")
        preview.capture(terminal, output, "closed-all-filtered", args)
        search(terminal, "flow-")
        wait_count(terminal, 47, 44)
        search(terminal, "")
        click_header(terminal, words, "chain")
        y = table_header_y(terminal, words)
        chain_ascending = re.compile(re.escape(words["connections.chain"]) + r"\s*▲")
        assert chain_ascending.search("".join(terminal.screen.cells[y])), "archived chains did not sort"
        click_header(terminal, words, "download_speed")
        assert chain_ascending.search("".join(terminal.screen.cells[y])), "history sorted on an unavailable live speed"
        click_header(terminal, words, "chain")
        assert "▼" in "".join(terminal.screen.cells[y]), "archived chain sort did not reverse"
        click_header(terminal, words, "chain")
        assert not any(arrow in "".join(terminal.screen.cells[y]) for arrow in "▲▼"), "archived chain sort did not reset"
        for width, height in SIZES:
            resize_terminal(terminal, words, width, height)
            terminal.key(b"\x1b[H")
            terminal.settle()
            check_tabs(terminal, words, 0, 47)
            wait_count(terminal, 47)
            header_totals(terminal, words)
            check_filter_layout(terminal, words, 0, 47, closed_view=True)
            if width >= 160:
                terminal.wait(words["navigation.connections.action.details"])
                terminal.settle()
            footer = terminal.screen.text().splitlines()[-1]
            if width >= 160:
                for hint_key in ("navigation.connections.action.details", "navigation.connections.search", "navigation.connections.active", "navigation.connections.browse", "navigation.connections.sort", "dashboard.footer.help"):
                    assert words[hint_key] in footer, "closed list lost its available action hint: " + hint_key
            assert not re.search(r"(?:^| · )x\s", footer), "closed list still advertises closing connections"
            preview.capture(terminal, output, "closed-%dx%d" % (width, height), args)
        search(terminal, "streaming")
        terminal.key(b"p")
        wait_header_pause(terminal, words, paused=True)
        click_cell(terminal, *header_bulk_action(terminal, words, closed_view=True, paused=True))
        wait_count(terminal, 0, 0)
        check_tabs(terminal, words, 0, 0)
        terminal.key(b"p")
        wait_header_pause(terminal, words)
        search(terminal, "")
        terminal.wait(words["connections.closed_empty"])
        check_filter_layout(terminal, words, 0, 0, closed_view=True)
        preview.capture(terminal, output, "closed-history-cleared", args)
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
        print("PASS %s/%s: case/Unicode whole-word/regex search, invalid-expression recovery, search-control mouse and bidirectional Tab at every size, rate samples, live pause, clickable table headers and keyboard sort, details/two-row clicks, fixed toolbar, six sizes, header pause/totals, framed search with outside count, active/closed tabs and frozen history, exact close, archived filters/details, close-all cancellation/confirmation; %s" %
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
