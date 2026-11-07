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


MEMBERS = ("AUTO", "HK 01", "HK 02", "JP 01", "SG 01", "US 01", "DIRECT", "REJECT")
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


def paste(terminal, value):
    terminal.key(b"\x1b[200~" + value.encode() + b"\x1b[201~")


def search(terminal, value):
    terminal.key(b"/")
    terminal.key(b"\x15")
    paste(terminal, value)
    terminal.key(b"\r")
    terminal.settle()


def chosen(terminal, name):
    return any(re.search(r"▌\s*(?:›\s*)?" + re.escape(name) + r"(?:\s|$)", line)
               for line in terminal.screen.text().splitlines())


def wait_mode(terminal, label):
    def selected():
        if re.search(r"<\s*" + re.escape(label) + r"\s*>", terminal.screen.text()):
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


def candidate_position(terminal, name):
    rows = terminal.screen.cells
    for y, row in enumerate(rows[:-1]):
        following = "".join(rows[y + 1])
        if not CARD_META.search(following):
            continue
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(name):
                return x, y
    raise AssertionError("Candidate is not visible: " + name + "\n" + terminal.screen.text())


def click_cell(terminal, x, y):
    terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())


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


def assert_header_hover(terminal, name, words, profile):
    terminal.key(b"\x1b[H")
    terminal.settle()
    x, y, arrow, line = group_header(terminal, name)
    left, right = x - 2, arrow + 2
    badge = re.search(r"(\d+)[ \t]+[▸▾][ \t]*$", line)
    assert badge and y > 0, "header lacks right-aligned count and disclosure"
    row = terminal.screen.cells[y]
    excluded = set(range(cell_offset(row, badge.start(1)) - 1, cell_offset(row, badge.end(1)) + 1))
    points = [(left, y - 1), (x + len(name) + 2, y), (right - 1, y + 1)]
    for key in ("proxies.sort.delay", "proxies.test_group"):
        button_offset = line.find(words[key])
        assert button_offset >= 0, "expanded header lost its action: " + words[key]
        points.append((cell_offset(row, button_offset), y))
    terminal.key(b"\x1b[<35;1;1M")
    terminal.settle()
    normal = terminal.screen.styles[y - 1][left][1]
    for hx, hy in points:
        terminal.key(("\x1b[<35;%d;%dM" % (hx + 1, hy + 1)).encode())
        terminal.settle()
        assert group_header(terminal, name)[3].rstrip().endswith("▾"), "hover folded its group"
        if profile == "no-color":
            continue
        hover = terminal.screen.styles[y - 1][left][1]
        assert hover != normal, "header padding did not receive hover background"
        for row in range(y - 1, y + 2):
            for column in range(left, right):
                if row == y and column in excluded:
                    continue  # The numeric badge deliberately retains its panel.
                assert terminal.screen.styles[row][column][1] == hover, "header hover surface has an uncolored gap at %d,%d" % (column, row)


def click_candidate(terminal, name, padding=0):
    terminal.settle()
    x, y = candidate_position(terminal, name)
    click_cell(terminal, x, y + padding)


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
    if len(slots) > 1:
        width = min(b - a for a, b in zip(slots, slots[1:])) - 1
        return left, y, left + width
    # The blank top row has the same background across the complete card,
    # including its delay/action areas. It also works for filtered short groups.
    background = terminal.screen.styles[y - 1][left][1]
    right = left
    while right < terminal.screen.columns and terminal.screen.styles[y - 1][right][1] == background:
        right += 1
    if right < terminal.screen.columns:
        return left, y, right
    # NO_COLOR has no background boundary. At this wide filtered fixture the
    # known 12-cell lane still determines the observed right edge of the card.
    row = terminal.screen.cells[y]
    value = re.search(r"\d+ ms", "".join(row[x:]))
    assert value, "cannot locate the uncolored card's latency lane: " + name
    value_x = x + cell_offset(row[x:], value.start())
    right = value_x - (12 - len(value.group())) // 2 + 12 + 2
    return left, y, right


def delay_slot(terminal, name):
    left, y, right = card_bounds(terminal, name)
    width = min(12, (right - left - 6) // 2)
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
    ys = sorted({y for _, y in positions})
    assert all(b - a == 4 for a, b in zip(ys, ys[1:])), "card rows did not retain a four-line pitch: " + repr(positions)
    first_y = min(y for _, y in positions)
    gap = terminal.screen.cells[first_y - 2][xs[0] - 4:]
    assert not "".join(gap).strip(), "group header touches the first card row"
    return xs


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
        # The summary remains visible; a group click expands its candidates.
        open_group(terminal, "PROXY")
        terminal.wait("Shadowsocks")
        terminal.wait("Hysteria2")
        terminal.wait("UDP")
        assert words["proxies.sort.default"] not in terminal.screen.text(), "default order added an unnecessary row"
        assert "0/0" not in terminal.screen.text(), "idle measurement progress is visible"
        preview.capture(terminal, output, "groups-wide", args)

        # At 160 columns the adaptive grid has three candidate columns.
        # Horizontal keys move to the nearest card; Up keeps its column.
        terminal.key(b"\x1b[C")  # group -> AUTO, first left candidate
        terminal.key(b"\x1b[B")  # JP 01, second left candidate
        terminal.key(b"\x1b[C")  # SG 01, second middle candidate
        terminal.key(b"\x1b[A")  # HK 01, first middle candidate
        terminal.settle()
        assert any(re.search(r"›\s*(?:✓\s*)?HK 01", line) for line in terminal.screen.text().splitlines()), "keyboard could not reach the right candidate column"
        # Selection goes through the asynchronous backend and snapshot readback.
        terminal.key(b"\r")
        wait_until(terminal, lambda: chosen(terminal, "HK 01"), "manual choice was not confirmed")
        # Top padding and the metadata row's right padding select the card.
        click_candidate(terminal, "HK 02", padding=-1)
        wait_until(terminal, lambda: chosen(terminal, "HK 02"), "top padding did not select its card")
        terminal.settle()
        _, y, right = card_bounds(terminal, "HK 01")
        click_cell(terminal, right - 1, y + 1)
        wait_until(terminal, lambda: chosen(terminal, "HK 01"), "metadata right padding did not select its card")
        terminal.settle()
        x, y = candidate_position(terminal, "HK 01")
        assert all(terminal.screen.cells[row][x - 4] == "▌" for row in range(y - 1, y + 2)), "selection rail did not span the three-line card"
        preview.capture(terminal, output, "selected", args)
        terminal.key(b"i")
        terminal.wait("Shadowsocks")
        terminal.wait(words["proxies.core_target"])
        preview.capture(terminal, output, "node-details", args)
        terminal.key(b"\x1b")
        terminal.wait("Hysteria2")

        # The reference's independent action browses without selecting it.
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        enter_reference(terminal, "AUTO", words["proxies.enter"])
        terminal.wait(words["proxies.auto"])
        terminal.key(b"\x1b[C")
        terminal.key(b"\r")
        terminal.wait("Shadowsocks")
        terminal.wait(words["proxies.core_target"])
        preview.capture(terminal, output, "automatic-member-details", args)
        terminal.key(b"\x1b")
        terminal.key(b"\x1b[D")
        terminal.wait("Hysteria2")
        wait_until(terminal, lambda: chosen(terminal, "HK 01"), "reference return lost parent choice")

        # Search locates another group; browsing its reference clears the filter,
        # and returning restores it. Native search input owns typed shortcuts.
        search(terminal, "ChatGPT")
        terminal.wait("ChatGPT")
        terminal.key(b"\x1b[C")
        enter_reference(terminal, "PROXY", words["proxies.enter"])
        terminal.wait("Hysteria2")
        terminal.key(b"\x1b[D")
        terminal.wait("ChatGPT")
        wait_until(terminal, lambda: any(re.search(r"(?:^|\s)ChatGPT\s+" + re.escape(words["search.clear"]) + r"(?:\s|$)", line) for line in terminal.screen.text().splitlines()), "reference return lost search input")
        preview.capture(terminal, output, "search-reference-return", args)
        search(terminal, "")
        collapse_all(terminal, words)
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        terminal.wait("Hysteria2")

        # Sort is explicit, and measurements never reorder its frozen sequence.
        terminal.key(b"s")
        terminal.wait(words["proxies.sort.delay"])
        terminal.key(b"\x1b[B")
        terminal.key(b"\r")
        terminal.settle()
        before = candidate_order(terminal, words["proxies.enter"])
        assert len(before) == 8, "incomplete candidate order before measurement: " + repr(before)
        terminal.key(b"t")
        terminal.wait(words["proxies.cancel_test"])
        terminal.key(b"t")
        wait_until(terminal, lambda: words["proxies.cancel_test"] not in terminal.screen.text(), "measurement did not cancel")
        end = time.monotonic() + 0.25
        while time.monotonic() < end:
            terminal.read(0.05)
        assert "777 ms" not in terminal.screen.text(), "canceled result reached current view"
        preview.capture(terminal, output, "measurement-canceled", args)
        terminal.key(b"t")
        terminal.wait(words["proxies.cancel_test"])
        wait_until(terminal, lambda: words["proxies.cancel_test"] not in terminal.screen.text(), "measurement did not finish")
        terminal.wait("211 ms")
        terminal.wait(words["proxies.timeout"])
        completed_label = words["proxies.test.done"].split("%", 1)[0]
        terminal.wait(completed_label)
        terminal.settle()
        assert candidate_order(terminal, words["proxies.enter"]) == before, "measurement silently reordered candidates"
        assert "777 ms" not in terminal.screen.text(), "late canceled value leaked into completed result"
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
            if width in (200, 240):
                assert_four_columns(terminal)
            if width == 160:
                assert len({candidate_position(terminal, name)[0] for name in MEMBERS}) == 3, "160-column grid lost its three-column layout"
            if width < 100:
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

        # Short groups and member filters retain the same slots as a full grid.
        terminal.resize(240, 40)
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        terminal.settle()
        slots = assert_four_columns(terminal)
        left, _, right = card_bounds(terminal, "HK 01")
        card_width = right - left
        search(terminal, "Hong Kong")
        collapse_all(terminal, words)
        open_group(terminal, "Hong Kong")
        terminal.wait("Trojan")
        terminal.settle()
        assert sorted(candidate_position(terminal, name)[0] for name in ("HK 01", "HK 02")) == slots[:2], "two-member group stretched its cards"
        left, _, right = card_bounds(terminal, "HK 01")
        assert right - left == card_width, "short-group card width changed"
        preview.capture(terminal, output, "two-members-240x40", args)
        search(terminal, "HK 01")
        collapse_all(terminal, words)
        open_group(terminal, "PROXY")
        terminal.wait("Shadowsocks")
        terminal.settle()
        assert candidate_position(terminal, "HK 01")[0] == slots[0], "single filtered member moved out of its first slot"
        left, _, right = card_bounds(terminal, "HK 01")
        assert right - left == card_width, "single filtered member stretched across the row"
        preview.capture(terminal, output, "one-member-filter-240x40", args)
        search(terminal, "")
        collapse_all(terminal, words)
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        terminal.resize(80, 24)
        terminal.settle()
        terminal.key(b"\x1b[6~")  # Continue scrolling within one group, no page switch.
        terminal.settle()
        assert re.search(r"\d+–\d+ / 8", terminal.screen.text()), "scrolled group lost its visible range"
        preview.capture(terminal, output, "continuous-scroll-range", args)
        terminal.resize(160, 45)
        terminal.settle()

        # Group strategies are not drawn as one manually chosen server.
        search(terminal, "BALANCE")
        terminal.key(b"\x1b[C")
        terminal.wait(words["proxies.distributed"])
        preview.capture(terminal, output, "load-balance", args)
        search(terminal, "EMPTY")
        terminal.key(b"\x1b[C")
        terminal.wait(words["proxies.empty_group"])
        preview.capture(terminal, output, "empty-group", args)
        search(terminal, "")
        terminal.settle()
        # Global exposes GLOBAL and allows following its references. Returning
        # to Rule restores ordinary groups; Direct explains retained choices.
        terminal.click_text(words["dashboard.mode.global"])
        wait_mode(terminal, words["dashboard.mode.global"])
        terminal.wait("GLOBAL")
        terminal.settle()
        terminal.click_text("GLOBAL")
        terminal.key(b"\x1b[C")
        enter_reference(terminal, "PROXY", words["proxies.enter"])
        terminal.wait("Hysteria2")
        terminal.key(b"\x1b[D")
        terminal.wait("GLOBAL")
        preview.capture(terminal, output, "global-reference-return", args)
        terminal.click_text(words["dashboard.mode.direct"])
        wait_mode(terminal, words["dashboard.mode.direct"])
        terminal.wait(words["proxies.direct_mode"][:40])
        preview.capture(terminal, output, "direct-mode", args)
        terminal.click_text(words["dashboard.mode.rule"])
        wait_mode(terminal, words["dashboard.mode.rule"])
        collapse_all(terminal, words)
        terminal.wait("BALANCE")
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
        print("PASS %s/%s: selection/padding, references, search, sort, cancel/late results, strategies, resize/four-column cap" % (language, profile), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
        if args.png:
            preview.png(terminal.screen, output / "failed.png", args.font, args.cjk_font, args.font_size)
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
        open_group(terminal, "PROXY")
        open_group(terminal, "AUTO")
        assert all(group_header(terminal, name)[3].rstrip().endswith("▾") for name in ("PROXY", "AUTO")), "opening a second group collapsed the first"
        terminal.key(b"s")
        terminal.wait(words["proxies.sort.name"])
        terminal.click_text(words["proxies.sort.name"])
        terminal.settle()
        terminal.key(b"\x1b[H")
        terminal.key(b"s")
        terminal.wait(words["proxies.sort.delay"])
        terminal.click_text(words["proxies.sort.delay"])
        terminal.settle()
        assert words["proxies.sort.delay"] in group_header(terminal, "PROXY")[3], "PROXY lost its independent delay sort"
        assert words["proxies.sort.name"] in group_header(terminal, "AUTO")[3], "sorting PROXY replaced AUTO's name sort"
        preview.capture(terminal, output, "multiple-expanded-independent-sort", args)

        group_action(terminal, "PROXY", words["proxies.test_group"])
        wait_until(terminal, lambda: group_has_action(terminal, "PROXY", words["proxies.cancel_test"]), "PROXY test did not start")
        group_action(terminal, "AUTO", words["proxies.test_group"])
        wait_until(terminal, lambda: group_has_action(terminal, "AUTO", words["proxies.cancel_test"]) and group_has_action(terminal, "PROXY", words["proxies.test_group"]) and not group_has_action(terminal, "PROXY", words["proxies.cancel_test"]), "cross-group testing did not transfer the active round")
        preview.capture(terminal, output, "cross-group-testing", args)
        wait_until(terminal, lambda: group_has_action(terminal, "AUTO", words["proxies.test_group"]) and not group_has_action(terminal, "AUTO", words["proxies.cancel_test"]), "AUTO testing did not finish")
        terminal.settle()
        assert "777 ms" not in terminal.screen.text(), "old group test generation published a late result"

        group_action(terminal, "PROXY", words["proxies.test_group"])
        wait_until(terminal, lambda: group_has_action(terminal, "PROXY", words["proxies.cancel_test"]), "second PROXY test did not start")
        collapse_all(terminal, words)
        assert all(group_header(terminal, name)[3].rstrip().endswith("▸") for name in ("PROXY", "AUTO")), "collapse all left a group open"
        completed = words["proxies.test.done"] % (6, 6)
        terminal.wait(completed)
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
        print("PASS independent multi-group fixture: expansion, sort, cross-group cancellation/stale isolation, collapse-all preserves testing; " + json.dumps(calls, ensure_ascii=False), flush=True)
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
            exercise(preview, args.binary, args.output.resolve() / language / profile, language, profile, args)
    exercise_groups(preview, args.binary, args.output.resolve() / "multi-group", args)
    exercise_node_delay(preview, args.binary, args.output.resolve() / "node-delay", args)


if __name__ == "__main__":
    main()
