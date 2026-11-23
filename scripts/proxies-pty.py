#!/usr/bin/env python3
"""Verify Proxies using a real controlling PTY and an explicit Go test fixture.

Build the fixture with `go test -c ./internal/tui -o bin/proxies-tui.test`.
All backend state is in memory: no network, core installation, managed files,
system services or real proxy traffic are used. Optional PNGs redraw actual
PTY cells and attributes; they are not native terminal-app screenshots.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys
import time

from tui_layout import SIDEBAR_MIN_WIDTH, sidebar_visible


MEMBERS = ("AUTO", "HK 01", "HK 02", "JP 01", "SG 01", "US 01", "DIRECT", "REJECT")
MODES = ("rule", "global", "direct")
CARD_META = re.compile(r"Shadowsocks|Trojan|VLESS|Hysteria2|VMess|Proxy group|策略组|\bDirect\b|\bReject\b")


def wait_until(terminal, predicate, label, timeout=12):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        terminal.read(0.05)
        if not terminal.screen.synchronized and predicate():
            return
        if terminal.closed:
            break
    raise AssertionError(label + "\n" + terminal.screen.text())


def labels(language):
    result = {}
    directory = Path(__file__).resolve().parents[1] / "internal" / "i18n" / "locales"
    for path in directory.glob(language + ".*.json"):
        result.update(json.loads(path.read_text()))
    return result


def control_position(terminal, label):
    # Include wrapped modes in the fixed rows of a narrow terminal.
    for y, row in enumerate(terminal.screen.cells[:8]):
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(label):
                return x, y
    raise AssertionError("Missing proxy control: " + label + "\n" + terminal.screen.text())


def proxy_mode_faces(terminal, words):
    labels = [words["dashboard.mode." + mode] for mode in MODES]
    positions = [control_position(terminal, label) for label in labels]
    widths = [cell_offset(terminal.screen.cells[y][x:], len(label))
              for (x, y), label in zip(positions, labels)]
    longest = max(widths)
    return [(x - 1 - (longest - width) // 2, y, longest + 2)
            for (x, y), width in zip(positions, widths)]


def proxy_header_positions(terminal, words):
    title = control_position(terminal, words["dashboard.page.proxies"])
    try:
        provider = control_position(terminal, words["providers.title.proxies"])
    except AssertionError:
        provider = control_position(terminal, "≡")
    modes = []
    bottom = max(title[1], provider[1])
    if terminal.screen.columns >= 50:
        modes = [control_position(terminal, words["dashboard.mode." + mode]) for mode in MODES]
        bottom = max(bottom, *(y for _, y in modes))
    return title, modes, provider, bottom


def assert_proxy_header(terminal, words, direct=False):
    rows = terminal.screen.text().splitlines()
    _, modes, provider, bottom = proxy_header_positions(terminal, words)
    header = "\n".join(rows[:bottom + 1])
    assert words["dashboard.page.proxies"] in header, "proxy header lost its page title"
    assert words["header.refresh"] not in header and "↻" not in header, "proxy header retained a manual refresh action"
    assert not re.search(r"\b\d+\s+groups\b|\d+\s*个代理组", header), "proxy header retained the group total"
    if terminal.screen.columns >= 50:
        assert len({y for _, y in modes + [provider]}) == 1, "mode choices and provider action do not share a header row"
        direct_x, direct_y = modes[-1]
        row = terminal.screen.cells[direct_y]
        faces = proxy_mode_faces(terminal, words)
        assert all(x + width == next_x for (x, _, width), (next_x, _, _) in zip(faces, faces[1:])), (
            "mode segments are not contiguous and equal-width: %r" % (faces,))
        direct_width = cell_offset(row[direct_x:], len(words["dashboard.mode.direct"]))
        search_x = next((x for x in range(direct_x + direct_width, provider[0])
                         if row[x] and ("".join(row[x:]).startswith(words["proxies.search"]) or row[x] == "/")), None)
        if direct:
            assert search_x is None, "Direct mode retained a search action"
        else:
            assert search_x is not None, "search action is missing between routing modes and providers"
            # A utility label starts three cells into its own button. Direct
            # reserves the Search slot so that the mode faces cannot move.
            x, _, width = faces[-1]
            assert search_x - 3 - (x + width) == 4, "mode/search spacing changed"
        provider_label = words["providers.title.proxies"]
        if not "".join(terminal.screen.cells[provider[1]][provider[0]:]).startswith(provider_label):
            provider_label = "≡"
        provider_width = cell_offset(terminal.screen.cells[provider[1]][provider[0]:], len(provider_label))
        assert terminal.screen.columns - provider[0] - provider_width <= 4, "mode/provider cluster is not right-aligned"


def assert_compact_proxy_body(terminal, words, first_group):
    _, _, _, header_bottom = proxy_header_positions(terminal, words)
    x, first_y, _, _ = group_header(terminal, first_group)
    body = ["".join(row[max(0, x - 2):]).strip()
            for row in terminal.screen.cells[header_bottom + 1:-2]]
    assert "›" not in body, "proxy body retained a standalone focus-arrow row"
    assert not any(prefix in row for row in body for prefix in ("Direct mode:", "Direct 模式：")), "Direct explanation still occupies the proxy body"
    return first_y


def assert_direct_empty(terminal, words):
    assert_proxy_header(terminal, words, direct=True)
    description = words["proxies.direct.description"]
    title, _, _, header_bottom = proxy_header_positions(terminal, words)
    columns, rows = terminal.screen.columns, terminal.screen.rows
    sidebar = sidebar_visible(columns, rows)
    left = title[0] - 2 if sidebar else 2
    right = columns - (3 if sidebar else 2)
    # The content uses the full height below the fixed header and above the
    # one-line key hints. A header divider, when present, is outside that area.
    top = max(3 if rows >= 24 or columns < SIDEBAR_MIN_WIDTH else 2, header_bottom + 1)
    bottom = rows - 1
    for y in range(header_bottom + 1, min(5, bottom)):
        value = "".join(terminal.screen.cells[y][left:right]).strip()
        if value and set(value) == {"▔"}:
            top = max(top, y + 1)

    content = []
    state = None
    for y in range(top, bottom):
        row = terminal.screen.cells[y]
        value = "".join(row[left:right]).strip()
        if not value:
            continue
        if columns < 50 and all(words["dashboard.mode." + mode] in value for mode in ("rule", "global", "direct")):
            top = y + 1  # Narrow modes remain outside the centered message area.
            continue
        content.append(value)
        for x in range(left, right):
            cell = row[x]
            if cell and "".join(row[x:]).startswith(description):
                assert state is None, "Direct empty state is duplicated"
                state = (x, y, cell_offset(row[x:], len(description)))
                break
    assert content == [description], "Direct body retained groups, nodes or extra controls: " + repr(content)
    assert state is not None, "Direct empty state is missing\n" + terminal.screen.text()
    state_x, state_y, state_width = state
    horizontal_error = abs(2 * state_x + state_width - left - right) / 2
    vertical_error = abs(2 * state_y + 1 - top - bottom) / 2
    assert horizontal_error <= 1, (
        "Direct message is not horizontally centered: message=%s, body=%s" %
        (state, (left, top, right, bottom)))
    assert vertical_error <= 1, (
        "Direct message is not vertically centered below fixed controls: message=%s, body=%s" %
        (state, (left, top, right, bottom)))
    return {"body": [left, top, right, bottom], "message": list(state),
            "horizontal_error_cells": horizontal_error, "vertical_error_rows": vertical_error}


def chosen(terminal, name):
    return any(re.search(r"▌\s*(?:›\s*)?" + re.escape(name) + r"(?:\s|$)", line)
               for line in terminal.screen.text().splitlines())


def wait_mode(terminal, label):
    def selected():
        if re.search(re.escape(label) + r"[ \t]*●", terminal.screen.text()):
            return True
        for y, row in enumerate(terminal.screen.cells):
            for x, cell in enumerate(row):
                if cell and re.match(re.escape(label) + r"(?:\s|$)", "".join(row[x:])) and terminal.screen.styles[y][x][2]:
                    return True
        return False
    wait_until(terminal, selected, "routing mode was not confirmed: " + label)


def candidate_order(terminal, reference_label):
    rows = terminal.screen.text().splitlines()
    result = []
    for index, line in enumerate(rows[:-1]):
        if not (reference_label in rows[index + 1] or
                re.search(r"Shadowsocks|Trojan|VLESS|Hysteria2|VMess|\bDirect\b|\bReject\b", rows[index + 1])):
            continue
        found = [(line.index(name), name) for name in MEMBERS if name in line]
        result.extend(name for _, name in sorted(found))
    return result


def candidate_position(terminal, name, start_row=0):
    rows = terminal.screen.cells
    for y in range(start_row, len(rows) - 1):
        row = rows[y]
        following = "".join(rows[y + 1])
        if not CARD_META.search(following):
            continue
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(name):
                return x, y
    raise AssertionError("Candidate is not visible: " + name + "\n" + terminal.screen.text())


def click_cell(terminal, x, y):
    terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())


def hover_cell(terminal, x, y):
    terminal.key(("\x1b[<35;%d;%dM" % (x + 1, y + 1)).encode())
    terminal.settle()


def wheel_body(terminal, down=True):
    button = 65 if down else 64
    terminal.key(("\x1b[<%d;%d;%dM" % (button, terminal.screen.columns - 5, terminal.screen.rows // 2)).encode())
    terminal.settle()


def cell_offset(row, text_offset):
    consumed = 0
    for column, cell in enumerate(row):
        if not cell:
            continue
        if consumed >= text_offset:
            return column
        consumed += len(cell)
    return len(row)


def group_header(terminal, name):
    for y, row in enumerate(terminal.screen.cells):
        line = "".join(row)
        match = re.search(r"(?<!\S)" + re.escape(name) + r"(?=[ \t]{2,})", line)
        arrow = re.search(r"[▸▾][ \t]*$", line)
        if match and arrow:
            return cell_offset(row, match.start()), y, cell_offset(row, arrow.start()), line
    raise AssertionError("Group header is not visible: " + name + "\n" + terminal.screen.text())


def open_group(terminal, name):
    terminal.settle()
    x, y, arrow, line = group_header(terminal, name)
    assert "▸" not in "".join(terminal.screen.cells[y][:x]) and "▾" not in "".join(terminal.screen.cells[y][:x]), "group retained a left disclosure arrow"
    if line.rstrip().endswith("▸"):
        click_cell(terminal, x, y)
        terminal.settle()


def collapse_all(terminal, words):
    # Completed selection notices can cover the toolbar's right-hand action.
    # Close their visible controls before deciding that no action is present.
    for _ in range(8):
        terminal.settle()
        if "×" not in terminal.screen.text():
            break
        terminal.click_text("×")
    else:
        raise AssertionError("selection notices still obstruct the toolbar")
    if words["proxies.collapse_all"] in terminal.screen.text():
        terminal.click_text(words["proxies.collapse_all"])
        terminal.settle()
    assert words["proxies.collapse_all"] not in terminal.screen.text(), "collapse-all action did not clear expansion state"
    assert not any(re.search(r"▾[ \t]*$", "".join(row)) for row in terminal.screen.cells), "collapse-all left a visible group open"


def browse_group(terminal, words, name):
    collapse_all(terminal, words)
    terminal.key(b"\x1b[H")
    terminal.settle()
    # Move through collapsed headers so the viewport reveals off-screen groups.
    for _ in range(12):
        try:
            group_header(terminal, name)
        except AssertionError:
            terminal.key(b"\x1b[B")
            terminal.settle()
            continue
        open_group(terminal, name)
        return
    raise AssertionError("Group could not be reached: " + name + "\n" + terminal.screen.text())


def group_action(terminal, name, label):
    terminal.settle()
    _, y, _, line = group_header(terminal, name)
    offset = line.find(label)
    assert offset >= 0, "group action is absent: " + name + "/" + label
    click_cell(terminal, cell_offset(terminal.screen.cells[y], offset), y)


def group_has_action(terminal, name, label):
    try:
        return label in group_header(terminal, name)[3]
    except AssertionError:
        return False  # A resize/update can temporarily clear a header mid-frame.


def group_progress(terminal, words, name, total):
    try:
        line = group_header(terminal, name)[3]
    except AssertionError:
        return False
    return re.search(r"(?<!\S)" + re.escape(words["proxies.cancel_test"]) + r"\s+\d+/" + str(total) + r"\b", line)


def sort_menu_item(terminal, label):
    # A short localized label can also occur in a hint or in the body behind
    # the popup. Only use its framed menu row as the click target.
    item = re.compile(r"│[ ›✓]*(" + re.escape(label) + r") +│")
    for y, row in enumerate(terminal.screen.cells):
        match = item.search("".join(row))
        if match:
            return cell_offset(row, match.start(1)), y
    return None


def click_group_sort(terminal, words, name, order):
    label = words["proxies.sort." + order]
    group_action(terminal, name, words["proxies.sort"])
    wait_until(terminal, lambda: sort_menu_item(terminal, label), "sort menu did not show " + label)
    click_cell(terminal, *sort_menu_item(terminal, label))
    wait_until(terminal, lambda: group_has_action(terminal, name, label + " ↑"),
               "sort menu did not apply %s to %s" % (label, name))
    terminal.settle()


def assert_header_hover(terminal, name, words, profile):
    terminal.key(b"\x1b[H")
    terminal.settle()
    x, y, arrow, line = group_header(terminal, name)
    left, right = x - 2, arrow + 2
    count = re.search(r"(\d+)[ \t]+[▸▾][ \t]*$", line)
    assert count and y > 0, "header lacks right-aligned count and disclosure"
    cells = terminal.screen.cells[y]
    points = [(left, y - 1), (right - 1, y + 1), (x + len(name) + 2, y), (cell_offset(cells, count.start(1)), y)]
    for key in ("proxies.sort.delay", "proxies.test_group"):
        button_offset = line.find(words[key])
        assert button_offset >= 0, "expanded header lost its action: " + words[key]
        points.append((cell_offset(cells, button_offset), y))
    hover_cell(terminal, 0, 0)
    normal = terminal.screen.styles[y - 1][left][1]
    first_x, first_y = min((candidate_position(terminal, member) for member in MEMBERS), key=lambda p: (p[1], p[0]))
    assert first_y - card_vertical_padding(terminal) == y + 3, "first card surface is not one row below the group header"
    assert not "".join(terminal.screen.cells[y + 2][left:right]).strip(), "header-to-card gap has visible content"
    if profile != "no-color":
        card = terminal.screen.styles[first_y][first_x][1]
        assert normal != card == terminal.screen.styles[y + 3][first_x][1], "first card is missing its top padding"
    click_cell(terminal, first_x, y + 2)
    terminal.settle()
    assert group_header(terminal, name)[3].rstrip().endswith("▾"), "clicking the gutter folded the group"
    click_cell(terminal, first_x, y + 1)
    terminal.settle()
    assert group_header(terminal, name)[3].rstrip().endswith("▸"), "bottom padding did not fold the group"
    open_group(terminal, name)
    assert group_header(terminal, name)[1] == y, "reopening the group moved its heading"
    for hx, hy in points:
        hover_cell(terminal, hx, hy)
        assert group_header(terminal, name)[3].rstrip().endswith("▾"), "hover folded its group"
        if profile == "no-color":
            continue
        hover = terminal.screen.styles[y - 1][left][1]
        assert hover != normal, "header padding did not receive hover background"
        for row in (y - 1, y, y + 1):
            for column in range(left, right):
                assert terminal.screen.styles[row][column][1] == hover, "header hover surface has an uncolored gap at %d,%d" % (column, row)
        assert all(terminal.screen.styles[y + 2][c][1] == normal for c in range(left, right)), "gutter shares header hover"


def assert_group_types(terminal, words):
    type_ends, count_ends, disclosure = set(), set(), set()
    for name, kind in (("PROXY", "select"), ("AUTO", "url-test"), ("Hong Kong", "fallback"),
                       ("BALANCE", "load-balance"), ("CHAIN", "relay")):
        _, y, arrow, line = group_header(terminal, name)
        type_label = words["proxies.type." + kind]
        match = re.search(re.escape(type_label) + r"\s+(\d+)\s+▸\s*$", line)
        assert match, "missing type before the count for %s: %s" % (name, line)
        cells = terminal.screen.cells[y]
        type_ends.add(cell_offset(cells, match.start() + len(type_label)))
        count_ends.add(cell_offset(cells, match.end(1)))
        disclosure.add(arrow)
        type_x = cell_offset(cells, match.start())
        count_x = cell_offset(cells, match.start(1))
        assert terminal.screen.styles[y][count_x][1] == terminal.screen.styles[y][type_x][1], "count has a separate panel: " + name
        hover_cell(terminal, type_x, y)
        terminal.wait("type: " + kind)
        if kind == "relay":
            terminal.wait("已弃用" if type_label == "链式代理" else "deprecated")
    assert len(type_ends) == len(count_ends) == len(disclosure) == 1, "group suffix columns do not align"


def click_candidate(terminal, name):
    terminal.settle()
    x, y = candidate_position(terminal, name)
    click_cell(terminal, x, y)


def card_bounds(terminal, name):
    x, y = candidate_position(terminal, name)
    left = x - 4
    columns = set()
    for member in MEMBERS:
        try:
            columns.add(candidate_position(terminal, member)[0])
        except AssertionError:
            pass
    slots = sorted(columns)
    # All calling scenarios show at least two neighbouring slots. Infer their
    # width from the observed grid even in NO_COLOR.
    assert len(slots) >= 2, "cannot measure neighbouring node slots: " + name
    width = min(b - a for a, b in zip(slots, slots[1:])) - 1
    return left, y, left + width


def delay_slot(terminal, name):
    left, y, right = card_bounds(terminal, name)
    width = min(10, (right - left - 6) // 2)
    return right - 2 - width, y, width


def delay_label(terminal, name):
    x, y, width = delay_slot(terminal, name)
    return "".join(terminal.screen.cells[y][x:x + width]).strip()


def wait_delay(terminal, name, expected):
    def ready():
        try:
            return delay_label(terminal, name) == expected
        except AssertionError:
            return False  # Incremental redraw can briefly clear a card row.
    wait_until(terminal, ready, "delay did not become %r for %s" % (expected, name))


def click_delay(terminal, name):
    terminal.settle()
    x, y, width = delay_slot(terminal, name)
    click_cell(terminal, x + width // 2, y)


def assert_delay_centered(terminal, name):
    x, y, width = delay_slot(terminal, name)
    cells = terminal.screen.cells[y][x:x + width]
    first = next(index for index, cell in enumerate(cells) if cell != " ")
    last = next(index for index, cell in enumerate(reversed(cells)) if cell != " ")
    assert abs(first - last) <= 1, "delay is not centered in its card slot: %s %r" % (name, cells)


def assert_four_columns(terminal):
    positions = [candidate_position(terminal, name) for name in MEMBERS]
    xs = sorted({x for x, _ in positions})
    assert len(xs) == 4, "wide grid did not keep four columns: " + repr(positions)
    return xs


def card_vertical_padding(terminal):
    return int(terminal.screen.columns >= SIDEBAR_MIN_WIDTH and terminal.screen.rows > 24)


def assert_card_spacing(terminal, profile):
    positions = []
    for name in MEMBERS:
        try:
            positions.append(candidate_position(terminal, name))
        except AssertionError:
            continue  # A short view may clip cards that are not focused.
    ys = sorted({y for _, y in positions})
    padding = card_vertical_padding(terminal)
    stride = 2 + 3 * padding
    assert len(ys) >= 2 and all(b - a == stride for a, b in zip(ys, ys[1:])), (
        "unexpected spacing between card rows: " + repr(positions))
    if padding == 0:
        _, heading_y, _, _ = group_header(terminal, "PROXY")
        heading_rows = 2 if terminal.screen.columns < SIDEBAR_MIN_WIDTH else 1
        assert ys[0] == heading_y + heading_rows, "compact group retained a blank row before its first card"
        return
    first = min(x for x, _ in positions) - 4
    gap_row = terminal.screen.cells[ys[0] - 2]
    assert not "".join(gap_row[first:]).strip(), "group header touches the first card row"
    cells, styles = terminal.screen.cells, terminal.screen.styles
    for x, y in positions:
        assert not cells[y - 1][x].strip() and not cells[y + 2][x].strip(), "card padding contains text"
        if profile != "no-color":
            card = styles[y][x][1]
            assert styles[y - 1][x][1] == styles[y + 2][x][1] == card, "padding is outside the card surface"
            if y != ys[-1]:
                assert styles[y + 3][x][1] != card, "adjacent card surfaces lack their one-row gap"


def enter_reference(terminal, name, label):
    terminal.settle()
    rows = terminal.screen.cells
    for y, row in enumerate(rows[:-1]):
        following = "".join(rows[y + 1])
        if label not in following:
            continue
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(name):
                # The action is on this card's metadata row, before the next
                # card's contents; choose the nearest action to its right.
                for button in range(x, len(rows[y + 1])):
                    if "".join(rows[y + 1][button:]).startswith(label):
                        terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (button + 1, y + 2, button + 1, y + 2)).encode())
                        terminal.settle()
                        return
    raise AssertionError("Reference action is not visible: " + name + "\n" + terminal.screen.text())


def fixture_terminal(preview, binary, report, language, profile, width=160, height=45, extra_environment=None):
    environment = {"LAZYCLASH_PROXIES_PTY_REPORT": str(report),
                   "LAZYCLASH_PROXIES_PTY_LANGUAGE": language}
    environment.update(extra_environment or {})
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, str(width), str(height), profile,
        language, str(binary.resolve()), "-test.run=^TestProxiesPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(width, height)
    return terminal


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True, exist_ok=True)
    report = output / "calls.json"
    words = labels(language)
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"2")
        terminal.wait("PROXY")
        terminal.settle()
        assert_proxy_header(terminal, words)
        first_group_y = assert_compact_proxy_body(terminal, words, "PROXY")
        # The old page refresh shortcuts must neither read state nor fall
        # through to the global service-restart shortcut. Model tests count
        # backend calls; the real terminal must also retain the current view.
        before = terminal.screen.text()
        terminal.key(b"rg\x12")
        terminal.settle()
        assert terminal.screen.text() == before, "removed refresh shortcuts changed the proxy page"
        # Clicking expands candidates without moving the heading.
        assert_group_types(terminal, words)
        preview.capture(terminal, output, "directory-relay-type", args)
        hover_cell(terminal, 0, 0)
        open_group(terminal, "PROXY")
        terminal.wait("Shadowsocks")
        terminal.wait("Hysteria2")
        terminal.wait("UDP")
        _, group_y, _, heading = group_header(terminal, "PROXY")
        assert group_y == first_group_y and re.search(r"›\s*PROXY\b", heading), "expanding PROXY moved its heading or focus"
        assert candidate_position(terminal, "HK 01")[1] > group_y, "members are not below their heading"
        assert words["proxies.search_group"] % "PROXY" not in terminal.screen.text(), "visible heading has a duplicate group label"
        title, _, _, _ = proxy_header_positions(terminal, words)
        assert words["proxies.collapse_all"] in "".join(terminal.screen.cells[title[1]]), "collapse all is not on the title row"
        assert words["proxies.sort"] in heading, "expanded group does not expose default sorting"
        assert not re.search(r"\d+–\d+ / 8", terminal.screen.text()), "all members fit, but the page shows a visible range"
        assert "0/0" not in terminal.screen.text(), "idle measurement progress is visible"
        preview.capture(terminal, output, "groups-wide", args)

        # At 160 columns the adaptive grid has three candidate columns.
        # Horizontal keys move to the nearest card; Up keeps its column.
        terminal.key(b"\x1b[C")  # Enter the observed selection, AUTO.
        terminal.key(b"\x1b[B")  # JP 01, second left candidate
        terminal.key(b"\x1b[C")  # SG 01, second middle candidate
        terminal.key(b"\x1b[A")  # HK 01, first middle candidate
        terminal.settle()
        assert any(re.search(r"›\s*(?:✓\s*)?HK 01", line) for line in terminal.screen.text().splitlines()), "keyboard could not reach the right candidate column"
        # Selection goes through the asynchronous backend and snapshot readback.
        terminal.key(b"\r")
        wait_until(terminal, lambda: chosen(terminal, "HK 01"), "manual choice was not confirmed")
        terminal.settle()
        # Both added padding rows belong to the same node selection surface.
        padding = card_vertical_padding(terminal)
        x, y = candidate_position(terminal, "HK 02")
        click_cell(terminal, x - 1, y - padding)
        wait_until(terminal, lambda: chosen(terminal, "HK 02"), "top padding did not select its card")
        terminal.settle()
        _, y, right = card_bounds(terminal, "HK 01")
        click_cell(terminal, right - 1, y + 1 + padding)
        wait_until(terminal, lambda: chosen(terminal, "HK 01"), "bottom padding did not select its card")
        terminal.settle()
        x, y = candidate_position(terminal, "HK 01")
        top, bottom = y - padding, y + 1 + padding
        assert all(terminal.screen.cells[row][x - 4] == "▌" for row in range(top, bottom + 1)), "selection rail did not span the padded card"
        assert terminal.screen.cells[top - 1][x - 4] != "▌" and terminal.screen.cells[bottom + 1][x - 4] != "▌", "selection rail extends outside the card"
        preview.capture(terminal, output, "selected", args)
        before = terminal.screen.text()
        terminal.key(b"i")
        terminal.settle()
        assert terminal.screen.text() == before, "retired detail shortcut changed the manually selected node's view"

        # The reference's independent action browses without selecting it.
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        enter_reference(terminal, "AUTO", words["proxies.enter"])
        terminal.wait(words["proxies.type.url-test"])
        terminal.settle()
        auto_row = group_header(terminal, "AUTO")[1]
        jp_x, jp_y = candidate_position(terminal, "JP 01", auto_row + 1)
        click_cell(terminal, jp_x, jp_y)
        terminal.settle()
        assert candidate_position(terminal, "JP 01", auto_row + 1) == (jp_x, jp_y), "automatic member click left its group"
        assert re.search(r"›\s*JP 01", "".join(terminal.screen.cells[jp_y])), "automatic member click did not focus the node"
        assert chosen(terminal, "HK 02"), "automatic member click changed the core's current node"
        footer = "".join(terminal.screen.cells[-1])
        assert not any(hint in footer for hint in ("Enter Details", "i Details", "Enter 详情", "i 详情")), "footer retained a node detail shortcut"
        before = terminal.screen.text()
        terminal.key(b"\r")
        terminal.settle()
        assert terminal.screen.text() == before, "Enter left the automatic group's member list"
        terminal.key(b"i")
        terminal.settle()
        assert terminal.screen.text() == before, "retired detail shortcut changed the automatic group's view"
        preview.capture(terminal, output, "automatic-member-list", args)
        terminal.key(b"\x1b[D")
        terminal.wait("Hysteria2")
        wait_until(terminal, lambda: chosen(terminal, "HK 01"), "reference return lost parent choice")

        # Browsing a reference returns to the source group's expanded cards.
        browse_group(terminal, words, "ChatGPT")
        enter_reference(terminal, "PROXY", words["proxies.enter"])
        terminal.wait("Hysteria2")
        terminal.key(b"\x1b[D")
        terminal.wait("ChatGPT")
        terminal.settle()
        assert group_header(terminal, "ChatGPT")[3].rstrip().endswith("▾"), "reference return lost the source group's expansion"
        preview.capture(terminal, output, "group-reference-return", args)
        collapse_all(terminal, words)
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        terminal.wait("Hysteria2")

        # Sort is explicit, and measurements never reorder its frozen sequence.
        terminal.key(b"s")
        sort_label = words["proxies.sort.delay"]
        wait_until(terminal, lambda: sort_menu_item(terminal, sort_label), "sort menu did not show delay order")
        terminal.key(b"\x1b[B")
        terminal.key(b"\r")
        wait_until(terminal, lambda: group_has_action(terminal, "PROXY", sort_label + " ↑"),
                   "keyboard sorting did not apply delay order to PROXY")
        terminal.settle()
        before = candidate_order(terminal, words["proxies.enter"])
        assert len(before) == 8, "incomplete candidate order before measurement: " + repr(before)
        header_row = group_header(terminal, "PROXY")[1]
        first_card = candidate_position(terminal, before[0])
        terminal.key(b"t")
        wait_until(terminal, lambda: group_progress(terminal, words, "PROXY", 6), "missing inline group progress")
        assert group_header(terminal, "PROXY")[1] == header_row and candidate_position(terminal, before[0]) == first_card, "starting a measurement shifted its heading or cards"
        terminal.key(b"t")
        wait_until(terminal, lambda: not group_progress(terminal, words, "PROXY", 6), "measurement did not cancel")
        end = time.monotonic() + 0.25
        while time.monotonic() < end:
            terminal.read(0.05)
        assert "777 ms" not in terminal.screen.text(), "canceled result reached current view"
        preview.capture(terminal, output, "measurement-canceled", args)
        terminal.key(b"t")
        wait_until(terminal, lambda: group_progress(terminal, words, "PROXY", 6), "missing inline progress after retry")
        wait_until(terminal, lambda: not group_progress(terminal, words, "PROXY", 6), "measurement did not finish")
        terminal.wait("211 ms")
        terminal.wait(words["proxies.timeout"])
        completed_label = words["proxies.test.done"] % (6, 6)
        wait_until(terminal, lambda: group_has_action(terminal, "PROXY", completed_label), "completed measurement count left the group header")
        terminal.settle()
        after = candidate_order(terminal, words["proxies.enter"])
        assert after == before, "measurement silently reordered candidates: %r -> %r" % (before, after)
        assert "777 ms" not in terminal.screen.text(), "late canceled value leaked into completed result"
        if profile != "no-color":
            for name, muted in (("US 01", False), ("SG 01", True), ("AUTO", True)):
                name_x, y = candidate_position(terminal, name)
                delay_x, _, width = delay_slot(terminal, name)
                value_color = terminal.screen.styles[y][delay_x + width // 2][0]
                reference_row = y + 1 if muted else y
                expected_color = terminal.screen.styles[reference_row][name_x][0]
                assert value_color == expected_color, "delay uses an unexpected foreground: " + name
        preview.capture(terminal, output, "measurement-complete", args)
        wait_until(terminal, lambda: completed_label not in terminal.screen.text(), "completed measurement status stayed visible", timeout=5)
        terminal.wait("211 ms")
        terminal.settle()
        assert candidate_order(terminal, words["proxies.enter"]) == before, "clearing measurement status changed candidate order"
        preview.capture(terminal, output, "measurement-quiet", args)
        assert_header_hover(terminal, "PROXY", words, profile)
        preview.capture(terminal, output, "group-header-hover", args)
        terminal.key(b"\x1b[<35;1;1M")

        # Keep a node focused across terminal geometry changes. Each frame is
        # captured from the PTY after redraw rather than directly from Model.View.
        click_candidate(terminal, "HK 01")
        for width, height in [(160, 45), (200, 40), (240, 40), (120, 32), (96, 40), (80, 24), (60, 20), (40, 16), (120, 18)]:
            terminal.resize(width, height)
            terminal.settle()
            terminal.wait("HK 01")
            terminal.wait("ms")
            assert_proxy_header(terminal, words)
            assert_card_spacing(terminal, profile)
            if width in (200, 240):
                assert_four_columns(terminal)
            if width == 160:
                assert len({candidate_position(terminal, name)[0] for name in MEMBERS}) == 3, "160-column grid lost its three-column layout"
            if width < SIDEBAR_MIN_WIDTH:
                slots = {candidate_position(terminal, name)[0] for name in ("HK 01", "HK 02")}
                assert len(slots) == (2 if width >= 77 else 1), "compact grid ignored its available width: " + repr(slots)
            if width == 200:
                terminal.key(b"\x1b[C")  # HK 01 -> JP 01 -> AUTO, the fourth slot.
                terminal.key(b"\x1b[C")
                terminal.settle()
                assert any(re.search(r"›\s*AUTO", line) for line in terminal.screen.text().splitlines()), "horizontal navigation missed the fourth card"
                terminal.key(b"\x1b[D")
                terminal.key(b"\x1b[D")
                terminal.settle()
                assert any(re.search(r"›\s*HK 01", line) for line in terminal.screen.text().splitlines()), "horizontal navigation did not return from the fourth card"
            preview.capture(terminal, output, "selected-%dx%d" % (width, height), args)

        # Short groups retain the same slots and card widths as a full grid.
        terminal.resize(240, 40)
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        terminal.settle()
        slots = assert_four_columns(terminal)
        left, _, right = card_bounds(terminal, "HK 01")
        card_width = right - left
        browse_group(terminal, words, "Hong Kong")
        terminal.wait("Trojan")
        terminal.settle()
        assert sorted(candidate_position(terminal, name)[0] for name in ("HK 01", "HK 02")) == slots[:2], "two-member group stretched its cards"
        assert not re.search(r"1–2 / 2", terminal.screen.text()), "fully visible short group retained a range"
        left, _, right = card_bounds(terminal, "HK 01")
        assert right - left == card_width, "short-group card width changed"
        preview.capture(terminal, output, "two-members-240x40", args)
        collapse_all(terminal, words)
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        terminal.resize(40, 16)
        terminal.settle()
        terminal.key(b"\x1b[6~")  # Continue scrolling within one group, no page switch.
        terminal.settle()
        range_pattern = re.escape(words["proxies.range.group"]).replace("%s", r"\d+–\d+ / 8")
        assert re.search(range_pattern, terminal.screen.text()), "scrolled group lost its member range"
        preview.capture(terminal, output, "continuous-scroll-range", args)
        terminal.resize(160, 45)
        terminal.settle()

        # Group strategies are not drawn as one manually chosen server.
        browse_group(terminal, words, "BALANCE")
        terminal.wait(words["proxies.distributed"])
        preview.capture(terminal, output, "load-balance", args)
        browse_group(terminal, words, "EMPTY")
        terminal.wait(words["proxies.empty_group"])
        preview.capture(terminal, output, "empty-group", args)
        # Direct hides the list without erasing its expansion, sort or choice.
        # Keep the original three mode writes by checking Direct before Global.
        browse_group(terminal, words, "PROXY")
        terminal.wait("Hysteria2")
        terminal.settle()
        order_before_direct = candidate_order(terminal, words["proxies.enter"])
        assert len(order_before_direct) == 8, "Direct restoration fixture lost its initial candidates"
        terminal.click_text(words["dashboard.mode.direct"])
        wait_mode(terminal, words["dashboard.mode.direct"])
        terminal.wait(words["proxies.direct.description"])
        terminal.settle()
        assert_direct_empty(terminal, words)
        terminal.key(b"sti")
        terminal.settle()
        metrics = assert_direct_empty(terminal, words)
        (output / "direct-mode-centering.json").write_text(json.dumps(metrics, indent=2) + "\n")
        preview.capture(terminal, output, "direct-mode", args)
        terminal.resize(40, 16)
        terminal.settle()
        terminal.wait(words["proxies.direct.description"])
        metrics = assert_direct_empty(terminal, words)
        (output / "direct-mode-40x16-centering.json").write_text(json.dumps(metrics, indent=2) + "\n")
        preview.capture(terminal, output, "direct-mode-40x16", args)
        terminal.resize(160, 45)
        terminal.settle()
        terminal.click_text(words["dashboard.mode.rule"])
        wait_mode(terminal, words["dashboard.mode.rule"])
        terminal.wait("Hysteria2")
        terminal.settle()
        assert group_header(terminal, "PROXY")[3].rstrip().endswith("▾"), "Direct return lost the expanded group"
        assert candidate_order(terminal, words["proxies.enter"]) == order_before_direct, "Direct return lost the candidate order"
        assert chosen(terminal, "HK 01"), "Direct return lost the selected node"
        assert words["proxies.sort.delay"] in group_header(terminal, "PROXY")[3], "Direct return lost its explicit sort"
        preview.capture(terminal, output, "direct-return-expanded", args)
        collapse_all(terminal, words)
        assert assert_compact_proxy_body(terminal, words, "PROXY") == first_group_y, "returning to Rule moved the first group row"

        # Global exposes GLOBAL and allows following its references.
        terminal.click_text(words["dashboard.mode.global"])
        wait_mode(terminal, words["dashboard.mode.global"])
        terminal.wait("GLOBAL")
        terminal.settle()
        assert_proxy_header(terminal, words)
        assert assert_compact_proxy_body(terminal, words, "GLOBAL") == first_group_y, "Global moved the first group row"
        terminal.click_text("GLOBAL")
        terminal.key(b"\x1b[C")
        enter_reference(terminal, "PROXY", words["proxies.enter"])
        terminal.wait("Hysteria2")
        terminal.key(b"\x1b[D")
        terminal.wait("GLOBAL")
        preview.capture(terminal, output, "global-reference-return", args)
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["selections"] == 3 and calls["selected"]["PROXY"] == "HK 01", calls
        assert calls["modes"] == 3, calls
        assert calls["canceled"] > 0 and calls["tests"] > calls["canceled"], calls
        assert 1 <= calls["max_tests"] <= 4, calls
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "application changed terminal palette"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styled text"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB text"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: plain page header, selection/padding, automatic node click/Enter stay in list, retired shortcuts, references, group navigation, sort, cancel/late results, strategies, centered Direct empty state/restoration, resize/four-column cap" % (language, profile), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
        if args.png:
            preview.png(terminal.screen, output / "failed.png", args.font, args.cjk_font, args.font_size)
        raise
    finally:
        terminal.cleanup()


def exercise_mode_positions(preview, binary, output, language, profile, args):
    output.mkdir(parents=True, exist_ok=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    sizes = ((160, 45), (53, 20), (40, 16), (20, 16))
    sequence = ("global", "direct", "rule", "direct", "global", "rule")
    geometry = {}
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"2")
        terminal.wait("PROXY")
        for width, height in sizes:
            terminal.resize(max(40, width), height)
            terminal.settle()
            open_group(terminal, "PROXY")
            if width < 50:
                terminal.wait(words["proxies.search_group"] % "PROXY")
            terminal.resize(width, height)
            terminal.settle()
            faces = proxy_mode_faces(terminal, words)
            if width == 20:
                assert len({y for _, y, _ in faces}) > 1, "narrow fixture did not wrap its mode segments"
            geometry["%dx%d" % (width, height)] = dict(zip(MODES, faces))
            for i, mode in enumerate(sequence):
                x, y, w = faces[MODES.index(mode)]
                # Alternate original left and right edges. Never search for a
                # relocated control or dismiss a notification between clicks.
                click_cell(terminal, x if i < 3 else x + w - 1, y)
                wait_mode(terminal, words["dashboard.mode." + mode])
                terminal.settle()
                observed = proxy_mode_faces(terminal, words)
                assert observed == faces, "mode controls moved at %dx%d after %s: %r -> %r" % (
                    width, height, mode, faces, observed)
                assert terminal.screen.cells[y][x] == "›", "clicked mode lost keyboard focus: " + mode
                if width in (160, 20) and i == 1:
                    preview.capture(terminal, output, "direct-%dx%d" % (width, height), args)
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["mode_writes"] == list(sequence) * len(sizes), calls
        assert calls["selections"] == calls["tests"] == 0, calls
        (output / "faces.json").write_text(json.dumps(geometry, indent=2) + "\n")
        print("PASS %s/%s: %d mode writes from fixed coordinates, with wrapped modes and group context" %
              (language, profile, len(calls["mode_writes"])), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
        raise
    finally:
        terminal.cleanup()


def exercise_groups(preview, binary, output, args):
    output.mkdir(parents=True, exist_ok=True)
    language, profile = "zh-Hans", "truecolor"
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile, 200, 50)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"2")
        terminal.wait("PROXY")
        terminal.resize(120, 25)
        terminal.settle()
        first_y = group_header(terminal, "PROXY")[1]
        terminal.key(b"\r")
        terminal.settle()
        _, open_y, _, heading = group_header(terminal, "PROXY")
        assert open_y == first_y and heading.rstrip().endswith("▾"), "Enter moved the heading or did not expand it"
        group_label = words["proxies.search_group"] % "PROXY"
        assert group_label not in terminal.screen.text(), "keyboard expansion displayed a duplicate group label"
        preview.capture(terminal, output, "expanded-in-place-120x25", args)

        wheel_body(terminal)
        terminal.wait(group_label)
        assert not group_has_action(terminal, "PROXY", words["proxies.test_group"]), "sticky label appeared with the heading still visible"
        assert candidate_position(terminal, "US 01")[1] > 0, "sticky label appeared without the group's nodes"
        preview.capture(terminal, output, "sticky-scrolled-120x25", args)
        wheel_body(terminal, down=False)
        assert group_header(terminal, "PROXY")[1] == first_y, "scrolling back did not restore the heading"
        assert group_label not in terminal.screen.text(), "sticky group label remained when the heading returned"

        # The directory still scrolls after this expanded group has passed.
        terminal.key(b"\x1b[F")
        terminal.settle()
        assert group_header(terminal, "EMPTY")[3].rstrip().endswith("▸"), "fixture did not scroll to later groups"
        assert not candidate_order(terminal, words["proxies.enter"]), "fixture still contains nodes from the first group"
        assert group_label not in terminal.screen.text(), "sticky group label remained after the last node"
        collapse_all(terminal, words)
        assert group_header(terminal, "PROXY")[1] == first_y, "collapse all did not restore the original directory row"

        terminal.resize(80, 24)
        terminal.settle()
        open_group(terminal, "PROXY")
        terminal.wait(group_label)
        assert proxy_header_positions(terminal, words)[3] < group_header(terminal, "PROXY")[1], "narrow group heading is missing from the member view"
        assert words["proxies.collapse_all"] not in terminal.screen.text(), "narrow member view has a collapse-all action"
        preview.capture(terminal, output, "narrow-members-80x24", args)
        terminal.key(b"\x1b")
        terminal.settle()
        assert group_label not in terminal.screen.text() and group_header(terminal, "PROXY")[3].rstrip().endswith("▸"), "leaving the member view kept its group label or expansion"

        terminal.resize(200, 50)
        terminal.settle()
        open_group(terminal, "PROXY")
        _, auto_y, _, _ = group_header(terminal, "AUTO")
        open_group(terminal, "AUTO")
        assert group_header(terminal, "AUTO")[1] == auto_y, "opening a second group moved its heading"
        assert all(group_header(terminal, name)[3].rstrip().endswith("▾") for name in ("PROXY", "AUTO")), "opening a second group collapsed the first"
        assert not any(words["proxies.search_group"] % name in terminal.screen.text() for name in ("PROXY", "AUTO")), "visible groups retained a sticky label"
        terminal.resize(120, 28)
        terminal.settle()
        prefix, suffix = (words["proxies.range.expanded"] % (2, "@")).split("@")
        assert re.search(re.escape(prefix) + r"\d+–\d+ / 12" + re.escape(suffix), terminal.screen.text()), "combined range omits its two groups"
        preview.capture(terminal, output, "two-group-member-range", args)
        terminal.resize(200, 50)
        terminal.settle()
        click_group_sort(terminal, words, "AUTO", "name")
        terminal.key(b"\x1b[H")
        terminal.settle()
        click_group_sort(terminal, words, "PROXY", "delay")
        assert group_has_action(terminal, "AUTO", words["proxies.sort.name"] + " ↑"), "sorting PROXY replaced AUTO's name sort"
        preview.capture(terminal, output, "multiple-expanded-independent-sort", args)

        group_action(terminal, "PROXY", words["proxies.test_group"])
        wait_until(terminal, lambda: group_progress(terminal, words, "PROXY", 6), "PROXY test did not start")
        group_action(terminal, "AUTO", words["proxies.test_group"])
        wait_until(terminal, lambda: group_progress(terminal, words, "AUTO", 4) and not group_progress(terminal, words, "PROXY", 6), "cross-group testing did not transfer the active round")
        preview.capture(terminal, output, "cross-group-testing", args)
        wait_until(terminal, lambda: group_has_action(terminal, "AUTO", words["proxies.test.done"] % (4, 4)), "AUTO testing did not finish")
        terminal.settle()
        assert "777 ms" not in terminal.screen.text(), "old group test generation published a late result"

        group_action(terminal, "PROXY", words["proxies.test_group"])
        wait_until(terminal, lambda: group_progress(terminal, words, "PROXY", 6), "second PROXY test did not start")
        collapse_all(terminal, words)
        assert all(group_header(terminal, name)[3].rstrip().endswith("▸") for name in ("PROXY", "AUTO")), "collapse all left a group open"
        completed = words["proxies.test.done"] % (6, 6)
        wait_until(terminal, lambda: group_has_action(terminal, "PROXY", completed), "collapsed group lost its measurement result")
        terminal.settle()
        assert "777 ms" not in terminal.screen.text(), "collapse all admitted an old generation's result"
        preview.capture(terminal, output, "all-collapsed-testing-complete", args)
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["selections"] == 0 and calls["modes"] == 0, calls
        # Canceled fixture calls deliberately return 80 ms late, so callbacks
        # from two generations can overlap. The six isolated runs retain the
        # four-request assertion for a single generation.
        assert calls["tests"] >= 14 and calls["canceled"] > 0 and calls["max_tests"] <= 8, calls
        assert calls["selected"]["PROXY"] == "AUTO", calls
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS independent multi-group fixture: anchored expansion, sticky and narrow labels, sort, cross-group cancellation/stale isolation, collapse-all preserves testing; " + json.dumps(calls, ensure_ascii=False), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
        if args.png:
            preview.png(terminal.screen, output / "failed.png", args.font, args.cjk_font, args.font_size)
        raise
    finally:
        terminal.cleanup()


def exercise_node_delay(preview, binary, output, args):
    output.mkdir(parents=True, exist_ok=True)
    language, profile = "zh-Hans", "truecolor"
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile,
        extra_environment={"LAZYCLASH_PROXIES_PTY_RETEST": "1"})
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"2")
        terminal.wait("PROXY")
        open_group(terminal, "PROXY")
        terminal.wait("Hysteria2")
        terminal.settle()
        for name in MEMBERS:
            assert_delay_centered(terminal, name)
        preview.capture(terminal, output, "centered-initial-values-statuses", args)

        # Existing and new values use the same independent, repeatable action.
        for expected in ("45 ms", "46 ms"):
            click_delay(terminal, "HK 01")
            wait_delay(terminal, "HK 01", words["proxies.testing"])
            assert_delay_centered(terminal, "HK 01")
            wait_delay(terminal, "HK 01", expected)
        click_delay(terminal, "US 01")
        wait_delay(terminal, "US 01", words["proxies.testing"])
        wait_delay(terminal, "US 01", "211 ms")

        # A reference's delay retries failures without browsing or selecting it.
        for _ in range(2):
            click_delay(terminal, "AUTO")
            wait_delay(terminal, "AUTO", words["proxies.testing"])
            wait_delay(terminal, "AUTO", words["proxies.test_failed"])
        for name in ("DIRECT", "REJECT"):
            assert delay_label(terminal, name) == "—", "built-in has a testable delay"
            click_delay(terminal, name)
        terminal.settle()
        assert chosen(terminal, "AUTO"), "delay click changed the selected proxy"
        assert group_header(terminal, "AUTO")[3].rstrip().endswith("▸"), "reference delay click entered its group"
        assert group_header(terminal, "PROXY")[3].rstrip().endswith("▾"), "delay click left its parent group"
        for name in MEMBERS:
            assert_delay_centered(terminal, name)
        preview.capture(terminal, output, "centered-retested-values-failed-reference", args)
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["requests"] == {"PROXY/HK 01": 2, "PROXY/US 01": 1, "PROXY/AUTO": 2}, calls
        assert calls["tests"] == 5 and calls["canceled"] == 0 and calls["max_tests"] == 1, calls
        assert calls["selections"] == 0 and calls["modes"] == 0 and calls["selected"]["PROXY"] == "AUTO", calls
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS independent node-delay fixture: centered values/statuses, repeat test, untested/failed actions, reference isolation, inert built-ins; " + json.dumps(calls, ensure_ascii=False), flush=True)
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
    spec = importlib.util.spec_from_file_location("proxies_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "256", "no-color"):
            output = args.output.resolve() / language / profile
            exercise(preview, args.binary, output, language, profile, args)
            exercise_mode_positions(preview, args.binary, output / "mode-switches", language, profile, args)
    exercise_groups(preview, args.binary, args.output.resolve() / "multi-group", args)
    exercise_node_delay(preview, args.binary, args.output.resolve() / "node-delay", args)


if __name__ == "__main__":
    main()
