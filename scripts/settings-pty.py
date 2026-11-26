#!/usr/bin/env python3
"""Verify Settings in a real controlling PTY with an opt-in memory backend.

Build: go test -c ./internal/tui -o bin/settings-tui.test
No core, network, managed files or service is used. Optional PNGs redraw actual
PTY cells and colors; they are not native terminal-app screenshots.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys
import time
import uuid

from tui_layout import dropdown_option, dropdown_trigger, sidebar_visible


SIZES = ((240, 45), (200, 40), (160, 45), (120, 32), (105, 32), (104, 32),
         (100, 28), (99, 28), (84, 24), (83, 24), (80, 24), (60, 20), (40, 16), (240, 45))


def labels(language):
    result = {}
    directory = Path(__file__).resolve().parents[1] / "internal" / "i18n" / "locales"
    for path in directory.glob(language + ".*.json"):
        result.update(json.loads(path.read_text()))
    return result


def position(terminal, label):
    for y, row in enumerate(terminal.screen.cells):
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(label):
                return x, y
    raise AssertionError("Visible text missing: " + label + "\n" + terminal.screen.text())


def click(terminal, point):
    x, y = point
    terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())


def check_dialog_heading(terminal, words, title, body):
    # The account editor and its settings row have the same title. Match it
    # above the dialog body, at the body's left inset.
    x, body_y = position(terminal, words[body])
    matches = [y for y, row in enumerate(terminal.screen.cells[:body_y])
               if "".join(row[x:]).startswith(words[title])]
    assert matches, (title, x, body_y)
    y = matches[-1]
    # At a two-cell inset the no-color modal outline occupies the outer cell.
    assert "".join(terminal.screen.cells[y][x-2:x]) in ("  ", "│ "), "unexpected title marker"
    assert terminal.screen.cells[y+1][x] == "─", "missing divider below title"


def check_port_row(terminal, words, key):
    x, y = position(terminal, words["access." + key])
    row = terminal.screen.cells[y]
    left = row.index("│", x)
    right = row.index("│", left + 1)
    value = "7897" if key == "mixed" else words["access.disabled"]
    assert "".join(row[left+1:right]).strip().startswith(value), (key, y, row[left:right+1])
    caption = "".join(row[x:left]).strip()
    expected = words["access." + key] + ("" if key == "http" else " ⓘ")
    assert caption == expected, (key, caption)
    if key == "mixed":
        assert "×" in row[left:right], "clear action must use ×"
    return left, right, y


def check_port_frames(terminal, fields, *, framed):
    rows = terminal.screen.cells
    for left, right, y in fields:
        corners = (rows[y-1][left], rows[y-1][right], rows[y+1][left], rows[y+1][right])
        if framed:
            assert corners == ("╭", "╮", "╰", "╯"), ("field frame", y, corners)
        else:
            assert corners[0] != "╭" and corners[2] != "╰", ("compact field", y, corners)


def check_wide_port_spacing(terminal, words, fields):
    check_port_frames(terminal, fields, framed=True)
    rows = terminal.screen.cells
    start, standard_y = position(terminal, words["access.standard"])
    _, transparent_y = position(terminal, words["access.transparent"])
    right = fields[0][1]
    # Only check cells inside the modal. The dashboard is still visible outside.
    def blank(y):
        assert not "".join(rows[y][start:right+1]).strip(), ("expected blank row", y)

    for heading, group in ((standard_y, fields[:3]), (transparent_y, fields[3:])):
        first_y = group[0][2]
        blank(heading + 1)
        assert first_y - 1 == heading + 2, ("heading to input", heading, first_y)
        for previous, current in zip(group, group[1:]):
            assert current[2] - previous[2] == 3, ("within group", previous, current)
    socks_bottom = fields[2][2] + 1
    blank(socks_bottom + 1)
    blank(socks_bottom + 2)
    assert transparent_y == socks_bottom + 3, ("between groups", socks_bottom, transparent_y)
    last_bottom = fields[-1][2] + 1
    blank(last_bottom + 1)
    assert rows[last_bottom+2][start] == "─", "missing footer separator after bottom margin"


def hover(terminal, x, y):
    terminal.key(("\x1b[<35;%d;%dM" % (x + 1, y + 1)).encode())
    terminal.settle()


def switch_track(enabled):
    return "░░░░██" if enabled else "██░░░░"


def card_bounds(screen, label_x):
    sidebar = sidebar_visible(screen.columns, screen.rows)
    left, end = (22, screen.columns - 3) if sidebar else (2, screen.columns - 2)
    if end - left >= 80:
        split = left + (end - left - 2) // 2
        if label_x < split:
            end = split
        else:
            left = split + 2
    return left, end


def check_card_stacks(terminal, words, columns):
    screen = terminal.screen
    starts = []
    for column in columns:
        previous = None
        for heading in column:
            x, y = position(terminal, words["settings." + heading])
            left, end = card_bounds(screen, x)
            assert x == left + 2, heading
            if previous is None:
                starts.append((x, y))
            else:
                prev_heading, prev_x, prev_y = previous
                assert x == prev_x and y > prev_y, heading + " is out of order"
                # Bottom padding, inter-card gap and top padding.
                assert all(not "".join(row[left:end]).strip() for row in screen.cells[y-3:y]), heading
                if prev_heading != "mihomo":
                    assert "".join(screen.cells[y-4][left:end]).strip(), heading + " has trailing blank rows"
            previous = heading, x, y
    if len(starts) == 2:
        (left_x, left_y), (right_x, right_y) = starts
        assert left_x < right_x and left_y == right_y


def check_switch(terminal, words, key, enabled, profile):
    label_x, y = position(terminal, words[key])
    row = terminal.screen.cells[y]
    _, right = card_bounds(terminal.screen, label_x)
    track_x = right - 8
    assert "".join(row[track_x:right]) == switch_track(enabled) + "  ", key
    before_track = "".join(row[label_x:track_x])
    annotated = key in ("dashboard.interface.keyHints", "settings.unifiedDelay", "settings.tcpConcurrent", "settings.allowLAN")
    label = words[key] + (" ⓘ" if annotated else "")
    assert before_track.startswith(label) and before_track[len(label):].strip(" ›") == "", (key, before_track)
    if profile != "no-color":
        styles = terminal.screen.styles[y]
        rail_x, thumb_x = (track_x, track_x + 4) if enabled else (track_x + 2, track_x)
        rail = styles[rail_x]
        assert rail[0] == rail[1] and rail[1] != styles[right-1][1], (key, "rail", rail)
        assert styles[thumb_x][0] != rail[0], (key, "thumb", styles[thumb_x])
    return track_x, y


def focus(terminal, label, row_label, footer_hint=""):
    pattern = re.compile(r"›\s*(?:[<\[]\s*)?" + re.escape(label))
    if label in (switch_track(True), switch_track(False)):
        # Several settings share the same rail. Match the focused rail on its own row.
        pattern = re.compile(re.escape(row_label) + r"[^\n]*› *" + re.escape(label))
    row_marker = re.compile(r"› +" + re.escape(row_label.split()[0]))
    for _ in range(36):
        terminal.settle()
        screen = terminal.screen.text()
        if terminal.screen.columns <= 40 and footer_hint:
            # A narrow footer may omit the focused action. Match the marker
            # to the control on its own row to distinguish identical switches.
            if any(row_label in row and label in row and (pattern.search(row) or row_marker.search(row))
                   for row in screen.splitlines()[:-1]):
                return
        if (pattern.search(screen) or row_marker.search(screen)) and label in screen and footer_hint in screen.splitlines()[-1]:
            return
        terminal.key(b"\t")
    raise AssertionError("Tab did not reveal " + label + "\n" + terminal.screen.text())


def dismiss_toasts(terminal):
    for _ in range(8):
        terminal.settle()
        # A save can publish its new snapshot before its busy footer closes.
        # Wait for the next action to be enabled before returning to the caller.
        if "Ctrl+C" in terminal.screen.text().splitlines()[-1]:
            terminal.wait_absent("Ctrl+C")
            continue
        if "×" not in terminal.screen.text():
            return
        click(terminal, position(terminal, "×"))
    raise AssertionError("Toasts still cover the Settings cards")


def fixture_terminal(preview, binary, report, language, profile):
    environment = {"LAZYCLASH_SETTINGS_PTY_REPORT": str(report), "LAZYCLASH_SETTINGS_PTY_LANGUAGE": language}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestSettingsPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def check_stable_hover(terminal, y, left, right, points):
    screen = terminal.screen
    cells, styles = screen.cells[y][left:right], screen.styles[y][left:right]
    for x in points:
        hover(terminal, x, y)
        assert screen.cells[y][left:right] == cells, ("hover changed row text", x, y)
        assert screen.styles[y][left:right] == styles, ("hover changed row style", x, y)


def check_tab_hover(preview, terminal, names, starts, left, right, profile, output, args):
    screen = terminal.screen
    x, y = position(terminal, names[0])
    selected = starts[2]
    widths = [preview.SMOKE.cell_width(name) for name in names]
    rail = ["─"] * (selected-x) + ["━"] * widths[2] + ["─"] * (right-2-selected-widths[2])
    hover(terminal, left, y)
    panel = screen.styles[y][left][1]
    labels = screen.cells[y][left:right]
    idle = screen.styles[y][left:right]
    rule = screen.styles[y+1][left:right]
    accent = screen.styles[y][selected][0]
    if profile != "no-color":
        assert all(style[1] == panel for style in idle + rule), "pointer-away left a highlighted cell"
        assert accent == screen.styles[y+1][selected][0] and accent[2] > accent[0]
        assert screen.styles[y][selected][2] and not screen.styles[y][x][2]

    for tab, state in ((0, "access-hover"), (2, "current-hover"), (None, "pointer-away")):
        if tab is None:
            points = [left]
        else:
            start, end = starts[tab]-1, starts[tab]+widths[tab]
            points = [start, end, starts[tab]]
        for target in points:
            hover(terminal, target, y)
            assert screen.cells[y][left:right] == labels, ("hover changed tab text", state, target)
            assert screen.cells[y+1][x:right-2] == rail, "hover moved the active underline"
            assert screen.styles[y+1][left:right] == rule, "hover recolored the underline"
            assert screen.styles[y][left:right] == idle, ("hover recolored a tab", state, target)
        preview.capture(terminal, output, "resources-focused-%s-%dx%d" % (state, screen.columns, screen.rows), args)


def check_tab_strip(preview, terminal, words, profile, output, args):
    terminal.resize(200, 46)
    terminal.settle()
    click(terminal, position(terminal, words["settings.tab.access"]))
    for width, height in ((200, 46), (40, 24)):
        terminal.resize(width, height)
        terminal.settle()
        names = [words["settings.tab." + tab] for tab in ("access", "network", "resources")]
        if names[0] not in terminal.screen.text():
            names = [words["settings.tab." + tab + ".compact"] for tab in ("access", "network", "resources")]
        # Reveal the complete Access pane in the single-column viewport.
        if width == 40:
            for _ in range(8):
                if position(terminal, names[0])[1] <= 6:
                    break
                terminal.key(b"\x1b[<65;37;12M")
                terminal.settle()
        x, y = position(terminal, names[0])
        left, right = card_bounds(terminal.screen, x)
        assert x == left + 2, "the first tab should start at the card inset"
        strip = "".join(terminal.screen.cells[y][x:right])
        starts = []
        for name in names:
            prefix, found, _ = strip.partition(name)
            assert found, name
            starts.append(x + preview.SMOKE.cell_width(prefix))
        gaps = [b - a - preview.SMOKE.cell_width(name) for a, b, name in zip(starts, starts[1:], names)]
        assert gaps[0] == gaps[1] and min(gaps) >= (4 if width == 200 else 1), gaps
        items = ["proxyInbound", "proxyPorts", "allowLAN", "bindAddress", "accessRules",
                 "proxyAuth", "proxyAddress", "controlAPI", "externalControl"]
        points = [position(terminal, words["settings." + item]) for item in items]
        offsets = [b[1]-a[1] for a, b in zip(points, points[1:])]
        assert all(delta >= (2 if width == 200 else 1) for delta in offsets), (points, offsets)
        assert points[2][0] == points[3][0] == points[4][0], "Allow LAN, bind address and access rules should align"
        address = "http://127.0.0.1:7897"
        assert address in terminal.screen.text(), "missing local proxy address"
        summary = words["settings.authNotConfigured"] + " ›"
        auth_x, auth_y = points[5]
        wraps = preview.SMOKE.cell_width(words["settings.proxyAuth"] + "  " + summary) > right-2-auth_x
        assert position(terminal, summary) == (right-2-preview.SMOKE.cell_width(summary), auth_y+int(wraps))
        # In the single column, the General card's height affects how many
        # wheel steps are needed to reveal Core Management.
        def next_card_position():
            steps = 0
            while width == 40 and words["settings.core"] not in terminal.screen.text() and steps < height:
                terminal.key(b"\x1b[<65;37;12M")
                terminal.settle()
                steps += 1
            row = position(terminal, words["settings.core"])[1]
            for _ in range(steps):
                terminal.key(b"\x1b[<64;37;12M")
                terminal.settle()
            return steps, row

        next_card_y = next_card_position()
        preview.capture(terminal, output, "ports-access-%dx%d" % (width, height), args)
        row_x, row_y = points[1]
        check_stable_hover(terminal, row_y, left, right, (row_x, right-3))
        switch_x, switch_y = check_switch(terminal, words, "settings.allowLAN", False, profile)
        check_stable_hover(terminal, switch_y, left, right, (switch_x, switch_x+5))
        # Keyboard focus stays on the selected tab as the mouse moves.
        for key in ("settings.ipv6", "settings.geoGroup"):
            terminal.key(b"\x1b[C")
            terminal.wait(words[key])
            terminal.settle()
            assert next_card_position() == next_card_y, "tab switch moved the next card"
        x, y = position(terminal, names[0])
        check_tab_hover(preview, terminal, names, starts, left, right, profile, output, args)
        click(terminal, (x, y))
        terminal.settle()
    terminal.resize(160, 45)
    terminal.settle()
    click(terminal, position(terminal, words["settings.tab.access"]))
    terminal.wait(words["settings.proxyPorts"])


def check_proxy_auth(preview, terminal, words, output, args):
    usernames = ("desktop", "tablet")
    passwords = [uuid.uuid4().hex[:14] for _ in usernames]

    def check_redaction():
        screen, stream = terminal.screen.text(), bytes(terminal.raw)
        assert all(secret not in screen and secret.encode() not in stream for secret in passwords), "proxy password displayed"

    def check_count(count):
        if count == 0:
            label = words["settings.authNotConfigured"]
        elif count == 1:
            label = words["settings.authOneUser"]
        else:
            label = words["settings.authUserCount"] % count
        terminal.wait(label + " ›")
        title = words["settings.proxyAuth"]
        x, y = position(terminal, title)
        _, end = card_bounds(terminal.screen, x)
        row = "".join(terminal.screen.cells[y][x:end])
        assert row[len(title):].strip() == label + " ›", row
        assert all(name not in row for name in usernames), "settings row exposed account details"

    def open_editor():
        terminal.click_text(words["settings.proxyAuth"])
        terminal.wait(words["access.authTitle"])
        terminal.wait(words["access.authAdd"])
        check_dialog_heading(terminal, words, "access.authTitle", "access.authHint")

    def review():
        terminal.click_text(words["ports.review"])
        terminal.wait(words["ports.reviewed"])
        terminal.settle()
        check_redaction()

    def apply_count(count):
        terminal.click_text(words["ports.apply_restart"])
        terminal.wait(words["ports.done"])
        terminal.click_text(words["ports.done"])
        terminal.wait_absent(words["ports.done"])
        dismiss_toasts(terminal)
        check_count(count)

    check_count(0)
    preview.capture(terminal, output, "proxy-auth-not-configured", args)
    open_editor()
    for index, (username, password) in enumerate(zip(usernames, passwords)):
        if index:
            terminal.click_text(words["access.authAdd"])
        terminal.key(username.encode())
        terminal.wait(username)
        terminal.key(b"\t" + password.encode())
        terminal.settle()
        check_redaction()
        account_row = next(row for row in terminal.screen.cells if username in "".join(row))
        assert "•" * len(password) in "".join(account_row), "wide account fields should share a row and mask the password"
    preview.capture(terminal, output, "proxy-auth-edit", args)
    terminal.click_text(words["access.authBypass"])
    terminal.wait("skip-auth-prefixes")
    terminal.key(b"\t192.0.2.0/24")
    terminal.wait("192.0.2.0/24")
    preview.capture(terminal, output, "auth-exceptions-160x45", args)
    terminal.resize(40, 24)
    terminal.settle()
    terminal.wait("192.0.2.0/24")
    assert words["ports.review"] in terminal.screen.text()
    preview.capture(terminal, output, "auth-exceptions-40x24", args)
    terminal.resize(160, 45)
    terminal.settle()
    review()
    terminal.wait("192.0.2.0/24")
    assert all(word in terminal.screen.text() for word in words["access.authPrefixAdded"].split())
    for username in usernames:
        terminal.wait(username)
    preview.capture(terminal, output, "proxy-auth-review", args)
    apply_count(2)
    preview.capture(terminal, output, "proxy-auth-count", args)
    open_editor()
    for username in usernames:
        terminal.wait(username)
    terminal.wait(words["access.authOnePrefix"])
    terminal.wait(words["access.authKeepPassword"])
    assert "•" * len(passwords[0]) not in terminal.screen.text(), "stored password was prefilled"
    terminal.resize(40, 16)
    terminal.settle()
    terminal.wait(words["access.authTitle"])
    check_redaction()
    preview.capture(terminal, output, "proxy-auth-40x16", args)
    for _ in range(6):
        if usernames[1] in terminal.screen.text():
            break
        terminal.key(b"\t")
        terminal.settle()
    assert usernames[1] in terminal.screen.text(), "compact viewport did not reveal the second account"
    terminal.resize(160, 45)
    terminal.settle()
    terminal.click_text(words["access.authRemove"])
    terminal.wait_absent(usernames[0])
    review()
    terminal.click_text(words["ports.cancel"])
    terminal.wait_absent(words["ports.reviewed"])
    terminal.wait(words["settings.proxyAuth"])
    terminal.settle()
    check_count(2)
    open_editor()
    for username in usernames:
        terminal.wait(username)
    for remaining, removed in ((1, usernames[0]), (0, usernames[1])):
        terminal.click_text(words["access.authRemove"])
        terminal.wait_absent(removed)
        review()
        if remaining == 0:
            terminal.wait(words["access.authDisabled"])
        apply_count(remaining)
        if remaining:
            open_editor()
            terminal.wait(usernames[1])
    check_redaction()


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("Settings preview")
        terminal.key(b"7")
        terminal.wait(words["settings.interface"])
        terminal.settle()
        for heading in ("settings.interface", "settings.mihomo", "settings.core"):
            terminal.wait(words[heading])
        terminal.wait("7897 ›")
        download = words["settings.downloadSettings"]
        method = words["wizard.method.direct"] + " ›"
        check_x, check_y = position(terminal, words["core_update.check"])
        download_x, download_y = position(terminal, download)
        assert download_x == check_x and download_y > check_y
        assert position(terminal, method)[1] == download_y
        assert "lazyclash doctor" not in terminal.screen.text()
        check_card_stacks(terminal, words, (("interface", "core"), ("mihomo",)))
        preview.capture(terminal, output, "settings-wide", args)
        click(terminal, (download_x, download_y))
        terminal.wait(words["wizard.method.environment"])
        preview.capture(terminal, output, "download-settings", args)
        terminal.key(b"\x1b")
        terminal.wait_absent(words["wizard.method.environment"])
        if not args.modals_only:
            check_tab_strip(preview, terminal, words, profile, output, args)
        if args.tabs_only:
            terminal.exit()
            print("PASS %s/%s: row spacing, tab spacing, keyboard selection and pointer hover at 200x46 and 40x24" % (language, profile), flush=True)
            return
        check_proxy_auth(preview, terminal, words, output, args)
        if args.auth_only:
            terminal.exit()
            assert json.loads(report.read_text())["auth_changes"] == [2, 1, 0]
            print("PASS %s/%s: proxy users, masked passwords, review, persistence, cancel, removal and compact viewport" % (language, profile), flush=True)
            return
        # A tall single column lets the same terminal frame expose the entire reading order.
        terminal.resize(80, 80)
        terminal.settle()
        check_card_stacks(terminal, words, (("interface", "mihomo", "core"),))
        preview.capture(terminal, output, "settings-single-column", args)
        terminal.resize(160, 45)
        terminal.settle()

        tab_y = position(terminal, words["settings.core"])[1]

        def select_tab(tab):
            # On a 160-cell canvas each tab has room for its first word.
            click(terminal, position(terminal, words["settings.tab." + tab].split()[0]))
            terminal.settle()
            assert position(terminal, words["settings.core"])[1] == tab_y, "tab switch moved the next card"

        click(terminal, position(terminal, words["settings.externalControl"]))
        terminal.wait(words["access.controlTitle"])
        terminal.wait("127.0.0.1:9097")
        check_dialog_heading(terminal, words, "access.controlTitle", "access.baseSection")
        preview.capture(terminal, output, "external-control-wide", args)
        terminal.click_text(words["access.tlsSection"])
        terminal.wait(words["access.cert"])
        preview.capture(terminal, output, "external-control-tls", args)
        terminal.resize(40, 24)
        terminal.settle()
        terminal.wait(words["ports.review"])
        preview.capture(terminal, output, "external-control-40x24", args)
        terminal.resize(160, 45)
        terminal.settle()
        terminal.click_text("127.0.0.1:9097")
        terminal.key(b"\x05\x15127.0.0.1:9098")
        terminal.key(b"\r")
        terminal.wait(words["ports.reviewed"])
        terminal.wait("127.0.0.1:9098")
        preview.capture(terminal, output, "external-control-review", args)
        terminal.click_text(words["ports.apply_restart"])
        terminal.wait(words["ports.done"])
        terminal.click_text(words["ports.done"])
        terminal.wait_absent(words["access.controlTitle"])
        dismiss_toasts(terminal)
        click(terminal, position(terminal, words["settings.proxyPorts"]))
        terminal.wait(words["access.proxyTitle"])
        terminal.wait(words["access.tproxy"])
        terminal.settle()
        keys = ("mixed", "http", "socks", "redir", "tproxy")
        check_dialog_heading(terminal, words, "access.proxyTitle", "access.standard")
        fields = [check_port_row(terminal, words, key) for key in keys]
        assert len({(left, right) for left, right, _ in fields}) == 1, fields
        ys = [y for _, _, y in fields]
        assert ys == sorted(set(ys)), fields
        assert position(terminal, words["access.standard"])[1] < ys[0]
        assert ys[2] < position(terminal, words["access.transparent"])[1] < ys[3]
        check_wide_port_spacing(terminal, words, fields)
        preview.capture(terminal, output, "proxy-ports-wide", args)
        tip_text = words["access.mixed.tooltip"][:12]
        label_x, label_y = position(terminal, words["access.mixed"])
        icon_x = terminal.screen.cells[label_y].index("ⓘ", label_x)
        hover(terminal, icon_x, label_y)
        terminal.wait(tip_text)
        preview.capture(terminal, output, "proxy-ports-tooltip", args)
        hover(terminal, label_x, label_y)
        terminal.wait_absent(tip_text)
        terminal.key(b"\x1bOP")  # F1 shows the focused field's explanation.
        terminal.wait(tip_text)
        terminal.key(b"\x1b")
        terminal.wait_absent(tip_text)
        terminal.wait(words["access.proxyTitle"])
        terminal.resize(200, 46)
        terminal.settle()
        wider = [check_port_row(terminal, words, key) for key in keys]
        assert len({(left, right) for left, right, _ in wider}) == 1, wider
        check_wide_port_spacing(terminal, words, wider)
        preview.capture(terminal, output, "proxy-ports-200x46", args)
        terminal.resize(40, 24)
        terminal.settle()
        mixed = check_port_row(terminal, words, "mixed")
        check_port_frames(terminal, [mixed], framed=False)
        left, right, _ = mixed
        terminal.wait(words["ports.review"])
        preview.capture(terminal, output, "proxy-ports-40x24", args)
        for key in keys[1:]:
            terminal.key(b"\t")
            terminal.settle()
            field = check_port_row(terminal, words, key)
            check_port_frames(terminal, [field], framed=False)
            assert field[:2] == (left, right), (key, field)
        terminal.resize(40, 16)
        terminal.settle()
        check_port_frames(terminal, [check_port_row(terminal, words, "tproxy")], framed=False)
        terminal.wait(words["ports.review"])
        preview.capture(terminal, output, "proxy-ports-40x16", args)
        terminal.resize(160, 45)
        terminal.settle()
        http_left, http_right, http_y = fields[1]
        click(terminal, ((http_left+http_right)//2, http_y))
        terminal.key(b"65536\r")
        terminal.wait(words["access.portRange"])
        _, error_y = position(terminal, words["access.portRange"])
        # A full-height field has a bottom border below the label/value row.
        assert error_y == position(terminal, words["access.http"])[1] + 2
        preview.capture(terminal, output, "proxy-ports-invalid", args)
        # Zero normalizes to the disabled placeholder when a field loses focus.
        terminal.key(b"\x05\x150")
        terminal.wait_absent(words["access.portRange"])
        mixed_left, _, mixed_y = check_port_row(terminal, words, "mixed")
        click(terminal, (mixed_left + 2, mixed_y))
        terminal.settle()
        check_port_row(terminal, words, "http")
        click(terminal, ((http_left+http_right)//2, http_y))
        terminal.key(b"8880\r")
        terminal.wait(words["ports.reviewed"])
        terminal.wait("8880")
        _, change_y = position(terminal, "8880")
        assert words["access.http"] in "".join(terminal.screen.cells[change_y])
        preview.capture(terminal, output, "proxy-ports-review", args)
        terminal.click_text(words["ports.cancel"])
        terminal.wait_absent(words["access.proxyTitle"])
        terminal.wait_absent(words["ports.reviewed"])
        terminal.settle()
        terminal.wait("7897 ›")
        if args.modals_only:
            terminal.exit()
            calls = json.loads(report.read_text())
            assert calls["auth_changes"] == [2, 1, 0] and calls["port_applies"] == 1, calls
            print("PASS %s/%s: three access dialogs, TLS disclosure, responsive field layout, review, apply and cancel" % (language, profile), flush=True)
            return
        for enabled in (False, True):
            click(terminal, check_switch(terminal, words, "settings.allowLAN", enabled, profile))
            if not enabled:
                terminal.wait("http://" + words["settings.lanIP"] + ":7897")
            dismiss_toasts(terminal)
            check_switch(terminal, words, "settings.allowLAN", not enabled, profile)
        select_tab("network")
        terminal.wait(words["settings.keepAliveIdle"])
        preview.capture(terminal, output, "settings-connections", args)
        tip_x, tip_y = position(terminal, words["settings.tcpConcurrent"])
        icon_x = terminal.screen.cells[tip_y].index("ⓘ", tip_x)
        tip_text = words["settings.tcpConcurrent.tooltip"][:8]
        hover(terminal, icon_x, tip_y)
        terminal.wait(tip_text)
        hover(terminal, tip_x, tip_y)
        terminal.wait_absent(tip_text)

        def set_number(key, value, formatted):
            click(terminal, position(terminal, words[key]))
            terminal.key(b"\x05\x15" + value.encode() + b"\r")
            terminal.wait(formatted)
            dismiss_toasts(terminal)

        set_number("settings.keepAliveIdle", "57", words["settings.seconds"] % "57")
        set_number("settings.keepAliveInterval", "9", words["settings.seconds"] % "9")
        switch_defaults = {
            "settings.ipv6": True, "settings.unifiedDelay": False,
            "settings.tcpConcurrent": False, "settings.tcpKeepAlive": True,
            "dashboard.interface.keyHints": True, "settings.autostart": True,
        }
        for key in ("dashboard.language.title", "settings.startPage", "settings.theme", "settings.logLevel", "settings.findProcessMode", *switch_defaults):
            x, y = position(terminal, words[key])
            row = terminal.screen.cells[y]
            left, right = card_bounds(terminal.screen, x)
            inset = 2 if key in {"dashboard.language.title", "settings.startPage", "settings.theme", "dashboard.interface.keyHints", "settings.autostart"} else 4
            assert x == left + inset and row[right-2:right] == [" ", " "], key
            if key in switch_defaults:
                check_switch(terminal, words, key, switch_defaults[key], profile)
            else:
                assert row[right-5] == "▾", "dropdown lacks its two-cell face and card insets: " + key

        select_tab("resources")
        terminal.wait(words["settings.geodataLoader"])
        preview.capture(terminal, output, "settings-resources", args)
        click(terminal, check_switch(terminal, words, "settings.geoAutoUpdate", False, profile))
        dismiss_toasts(terminal)
        set_number("settings.geoUpdateInterval", "48", words["settings.hours"] % "48")
        terminal.click_text(dropdown_trigger(terminal, words["settings.loaderMemory"]))
        terminal.click_text(dropdown_option(terminal, words["settings.loaderStandard"]))
        dismiss_toasts(terminal)
        click(terminal, check_switch(terminal, words, "settings.storeFakeIP", False, profile))
        dismiss_toasts(terminal)
        assert words["settings.etagHint"] not in terminal.screen.text()
        label_x, label_y = position(terminal, words["settings.etagSupport"])
        left, end = card_bounds(terminal.screen, label_x)
        check_stable_hover(terminal, label_y, left, end, (label_x, end-1))
        preview.capture(terminal, output, "settings-row-hover", args)
        click(terminal, (label_x, label_y))
        check_switch(terminal, words, "settings.etagSupport", True, profile)
        select_tab("network")

        native = {"en": "English", "zh-Hans": "简体中文"}
        current = dropdown_trigger(terminal, native[language])
        trigger_x, trigger_y = position(terminal, current)
        left, right = card_bounds(terminal.screen, trigger_x)
        check_stable_hover(terminal, trigger_y, left, right, (trigger_x,))
        terminal.click_text(current)
        dropdown_option(terminal, native[language], checked=True)
        terminal.key(b"\x1b")
        terminal.wait_absent("✓")
        terminal.resize(26, 24)
        terminal.settle()
        terminal.wait(current)
        label_x, label_y = position(terminal, words["dashboard.language.title"])
        trigger = position(terminal, current)
        # Two focus cells and two face-padding cells precede the value.
        assert trigger == (label_x + 4, label_y + 1), "wrapped dropdown did not start below its label"
        preview.capture(terminal, output, "settings-wrapped-26x24", args)
        click(terminal, trigger)
        dropdown_option(terminal, native[language], checked=True)
        terminal.key(b"\x1b")
        terminal.wait_absent("✓")
        terminal.key(b"\t")
        terminal.key(b"\r")
        dropdown_option(terminal, words["dashboard.page.overview"], checked=True)
        terminal.key(b"\x1b")
        terminal.wait_absent("✓")
        terminal.key(b"\t")
        terminal.settle()
        wrapped = position(terminal, switch_track(True))
        label = position(terminal, words["dashboard.interface.keyHints"].split()[0])
        assert wrapped[1] > label[1] and wrapped[0] == label[0] + 2, "switch did not wrap at the label inset"
        if profile == "no-color":
            assert terminal.screen.cells[label[1]][label[0]-2:label[0]] == ["›", " "]
            assert terminal.screen.cells[wrapped[1]][label[0]:wrapped[0]] == [" ", " "]
        else:
            assert terminal.screen.cells[wrapped[1]][label[0]:wrapped[0]] == ["›", " "]
        preview.capture(terminal, output, "switch-wrapped-26x24", args)
        click(terminal, wrapped)
        terminal.wait(switch_track(False))
        terminal.key(b"?")
        terminal.wait(switch_track(True))
        dismiss_toasts(terminal)
        terminal.resize(160, 45)
        terminal.settle()

        # The current IPv6 thumb, unified-delay rail and keyboard all activate
        # their own switch through the same controls users see in the cards.
        x, y = check_switch(terminal, words, "settings.ipv6", True, profile)
        left, right = card_bounds(terminal.screen, x)
        check_stable_hover(terminal, y, left, right, (x+5,))
        check_switch(terminal, words, "settings.ipv6", True, profile)
        click(terminal, (x + 5, y))
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.ipv6", False, profile)
        x, y = check_switch(terminal, words, "settings.unifiedDelay", False, profile)
        click(terminal, (x + 2, y))
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.unifiedDelay", True, profile)
        preview.capture(terminal, output, "mihomo-switches-wide", args)
        terminal.key(b"\r")
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.unifiedDelay", False, profile)

        terminal.click_text(dropdown_trigger(terminal, "info"))
        dropdown_option(terminal, "info", checked=True)
        for level in ("debug", "warning", "error", "silent"):
            dropdown_option(terminal, level)
        assert all(value not in terminal.screen.text() for value in ("Follow profile", "跟随配置"))
        preview.capture(terminal, output, "log-level-menu-wide", args)
        terminal.click_text(dropdown_option(terminal, "debug"))
        debug_trigger = dropdown_trigger(terminal, "debug")
        dismiss_toasts(terminal)

        for key in ("settings.tcpConcurrent", "settings.tcpKeepAlive"):
            point = check_switch(terminal, words, key, switch_defaults[key], profile)
            click(terminal, point)
            dismiss_toasts(terminal)
            check_switch(terminal, words, key, not switch_defaults[key], profile)

        select_tab("resources")
        click(terminal, check_switch(terminal, words, "settings.etagSupport", True, profile))
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.etagSupport", False, profile)
        select_tab("network")

        process_trigger = dropdown_trigger(terminal, words["settings.processAsNeeded"])
        terminal.click_text(process_trigger)
        dropdown_option(terminal, words["settings.processAsNeeded"], checked=True)
        dropdown_option(terminal, words["settings.processOff"])
        terminal.click_text(dropdown_option(terminal, words["settings.processAlways"]))
        process_trigger = dropdown_trigger(terminal, words["settings.processAlways"])
        dismiss_toasts(terminal)

        dark_trigger = dropdown_trigger(terminal, "Dark")
        terminal.click_text(dark_trigger)
        _, checked_y = position(terminal, dropdown_option(terminal, "Dark", checked=True))
        assert position(terminal, dropdown_option(terminal, "Light"))[1] == checked_y - 1, "Light must precede Dark"
        assert position(terminal, dropdown_option(terminal, "Auto"))[1] == checked_y + 1, "Auto must follow Dark"
        preview.capture(terminal, output, "theme-menu-wide", args)
        terminal.key(b"\x1b[A")
        terminal.key(b"\x1b")
        terminal.wait_absent("✓")
        terminal.wait(dark_trigger)

        terminal.key(b"\r")  # Canceling the dropdown returns focus to its trigger.
        dropdown_option(terminal, "Dark", checked=True)
        terminal.key(b"\x1b[A")
        terminal.key(b"\r")
        light_trigger = dropdown_trigger(terminal, "Light")
        terminal.wait(words["settings.themeLightPlaceholder"])
        terminal.click_text(light_trigger)
        dropdown_option(terminal, "Light", checked=True)
        mark = len(terminal.raw)
        terminal.click_text(dropdown_option(terminal, "Auto"))
        auto_trigger = dropdown_trigger(terminal, "Auto")
        terminal.wait(words["settings.themeAuto"] % "Dark")
        terminal.settle()
        assert b"\x1b]11;?" in terminal.raw[mark:], "Auto did not query the terminal background"
        terminal.send(b"\x1b]11;rgb:ffff/ffff/ffff\x07")
        terminal.wait(words["settings.themeAuto"] % "Light")
        terminal.click_text(auto_trigger)
        dropdown_option(terminal, "Auto", checked=True)
        terminal.click_text(dropdown_option(terminal, "Dark"))
        terminal.wait(dark_trigger)

        other = "zh-Hans" if language == "en" else "en"
        assert native[other] not in terminal.screen.text(), "closed language dropdown shows both languages"
        assert position(terminal, words["settings.startPage"])[1] == position(terminal, current)[1] + 2, "unexpected text below the language control"
        current_point = position(terminal, current)
        click(terminal, current_point)
        expanded = dropdown_trigger(terminal, native[language], expanded=True)
        assert position(terminal, expanded) == current_point
        dropdown_option(terminal, native[language], checked=True)
        pending_option = dropdown_option(terminal, native[other])
        pending_x, pending_y = position(terminal, pending_option)
        normal_style = terminal.screen.styles[pending_y][pending_x]
        preview.capture(terminal, output, "language-menu-wide", args)
        move = b"\x1b[B" if language == "en" else b"\x1b[A"
        terminal.key(move)
        terminal.settle()
        dropdown_option(terminal, native[language], checked=True)
        dropdown_option(terminal, native[other])
        if profile == "no-color":
            assert "›" in terminal.screen.cells[pending_y][:pending_x], "pending row lacks a visible cursor"
        else:
            assert terminal.screen.styles[pending_y][pending_x][1] != normal_style[1], "pending row did not highlight"
            assert "›" not in terminal.screen.cells[pending_y], "color dropdown shows a redundant cursor"
        preview.capture(terminal, output, "language-menu-pending", args)
        terminal.key(b"\x1b")
        terminal.wait_absent(native[other])
        preview.capture(terminal, output, "language-focused", args)
        terminal.key(b"\r")
        dropdown_option(terminal, native[language], checked=True)
        terminal.key(move)
        terminal.key(b"\r")
        click(terminal, current_point)  # The pending save disables the trigger.
        terminal.wait(labels(other)["settings.interface"])
        switched = dropdown_trigger(terminal, native[other])
        terminal.settle()
        preview.capture(terminal, output, "language-switched", args)
        terminal.click_text(switched)
        dropdown_option(terminal, native[other], checked=True)
        terminal.click_text(dropdown_option(terminal, native[language]))
        terminal.wait(words["settings.interface"])
        terminal.settle()

        # The explanation is available from the icon and the focused switch,
        # including when the footer itself is hidden.
        x, y = check_switch(terminal, words, "dashboard.interface.keyHints", True, profile)
        label_x, _ = position(terminal, words["dashboard.interface.keyHints"])
        icon_x = terminal.screen.cells[y].index("ⓘ", label_x, x)
        tip_text = words["settings.keyHints.tooltip"][:8]
        assert tip_text not in terminal.screen.text()
        assert position(terminal, words["settings.theme"])[1] == y + 2
        hover(terminal, icon_x, y)
        terminal.wait(tip_text)
        preview.capture(terminal, output, "footer-hints-tooltip", args)
        hover(terminal, label_x, y)
        terminal.wait_absent(tip_text)
        focus(terminal, switch_track(True), words["dashboard.interface.keyHints"], words["navigation.controls.hideHints"])
        click(terminal, (x + 4, y))
        terminal.settle()
        check_switch(terminal, words, "dashboard.interface.keyHints", False, profile)
        preview.capture(terminal, output, "hints-hidden", args)
        terminal.key(b"\x1bOP")
        terminal.wait(tip_text)
        terminal.key(b"\x1b")
        terminal.wait_absent(tip_text)
        terminal.key(b"?")
        terminal.settle()
        check_switch(terminal, words, "dashboard.interface.keyHints", True, profile)

        # Completed preference Toasts can cover the right card's heading.
        # Close them through their real mouse targets before measuring layout.
        dismiss_toasts(terminal)

        select_tab("access")
        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait(words["settings.interface"])
            sidebar = sidebar_visible(width, height)
            left, right = (22, width - 4) if sidebar else (2, width - 3)
            columns = 2 if right - left + 1 >= 80 else 1
            title_x, title_y = position(terminal, words["settings.interface"])
            assert title_x == left + 2
            headings = "".join(terminal.screen.cells[title_y][left:right+1])
            assert (words["settings.mihomo"] in headings) == (columns == 2)
            for row in terminal.screen.cells[title_y:-1]:
                cells = row[left:right+1]
                assert "┌" not in cells and "┐" not in cells, "card decorations in settings body"
                assert "─" not in cells or "━" in cells, "unexpected separator outside the tab strip"
            preview.capture(terminal, output, "settings-%dx%d" % (width, height), args)

            # Scroll down and up in the body. The short view must reveal lower
            # groups; each Tab pass then reveals the exact focused control.
            before = terminal.screen.text()
            for _ in range(8):
                terminal.key(("\x1b[<65;%d;%dM" % (width - 3, height // 2)).encode())
            terminal.settle()
            if columns == 1:
                assert before != terminal.screen.text(), "wheel did not scroll the narrow Settings body"
            preview.capture(terminal, output, "scrolled-%dx%d" % (width, height), args)
            if width == 40:
                focus(terminal, current, words["dashboard.language.title"])
                terminal.key(b"\r")
                dropdown_option(terminal, native[language], checked=True)
                preview.capture(terminal, output, "language-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent(native[other])
                focus(terminal, dark_trigger, words["settings.theme"])
                terminal.key(b"\r")
                dropdown_option(terminal, "Dark", checked=True)
                preview.capture(terminal, output, "theme-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent("✓")
                focus(terminal, "7897 ›", words["settings.proxyPorts"], words["navigation.controls.editPorts"])
                terminal.key(b"\x1b[Z")  # The active tab precedes its pane.
                terminal.key(b"\x1b[C")
                focus(terminal, debug_trigger, words["settings.logLevel"])
                terminal.key(b"\r")
                dropdown_option(terminal, "debug", checked=True)
                preview.capture(terminal, output, "log-level-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent("✓")
                terminal.settle()
                x, y = check_switch(terminal, words, "settings.ipv6", False, profile)
                click(terminal, (x, y))
                dismiss_toasts(terminal)
                check_switch(terminal, words, "settings.ipv6", True, profile)
                check_switch(terminal, words, "settings.unifiedDelay", False, profile)
                preview.capture(terminal, output, "mihomo-switches-40x16", args)
                focus(terminal, process_trigger, words["settings.findProcessMode"])
                terminal.key(b"\r")
                dropdown_option(terminal, words["settings.processAlways"], checked=True)
                preview.capture(terminal, output, "process-menu-40x16", args)
                terminal.key(b"\x1b")
                terminal.wait_absent("✓")
                focus(terminal, switch_track(True), words["settings.ipv6"])
                terminal.key(b"\x1b[Z")
                terminal.settle()
                terminal.key(b"\x1b[D")
                terminal.wait("7897 ›")
            focus(terminal, method, download)
            preview.capture(terminal, output, "tab-download-%dx%d" % (width, height), args)
            if width == 40:
                # Reopening immediately verifies the trigger kept keyboard focus.
                for _ in range(2):
                    terminal.key(b"\r")
                    terminal.wait(words["wizard.method.environment"])
                    terminal.key(b"\x1b")
                    terminal.wait_absent(words["wizard.method.environment"])
            focus(terminal, "7897 ›", words["settings.proxyPorts"], words["navigation.controls.editPorts"])
            terminal.key(b"\r")
            terminal.wait(words["access.proxyTitle"])
            terminal.wait("7897")
            preview.capture(terminal, output, "ports-%dx%d" % (width, height), args)
            terminal.key(b"\x1b")
            terminal.wait_absent(words["access.proxyTitle"])
            terminal.settle()
            assert re.search(r"› +" + re.escape(words["settings.proxyPorts"]), terminal.screen.text()), "port return lost its trigger focus"
            for _ in range(8):
                terminal.key(("\x1b[<64;%d;%dM" % (width - 3, height // 2)).encode())
            terminal.key(b"\x1b[H")
            terminal.settle()

        # Logs remains reachable directly from the main navigation.
        terminal.key(b"6")
        terminal.wait("settings-pty readonly log")
        preview.capture(terminal, output, "navigation-open-logs", args)
        terminal.key(b"7")
        terminal.wait(words["settings.interface"])
        terminal.settle()
        preview.capture(terminal, output, "logs-return-settings", args)

        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["language_saves"] == 2 and calls["language"] == language, calls
        assert calls["hint_saves"] == 4 and not calls["hints_hidden"], calls
        assert calls["theme_saves"] == 3 and calls["theme"] == "dark", calls
        assert calls["core_changes"] == [["keep-alive-idle", "57"], ["keep-alive-interval", "9"],
                                         ["geo-auto-update", "on"], ["geo-update-interval", "48"],
                                         ["geodata-loader", "standard"], ["profile.store-fake-ip", "on"],
                                         ["ipv6", "off"], ["unified-delay", "on"],
                                         ["unified-delay", "off"], ["log-level", "debug"],
                                         ["tcp-concurrent", "on"], ["disable-keep-alive", "on"],
                                         ["etag-support", "off"], ["find-process-mode", "always"],
                                         ["ipv6", "on"]], calls
        assert calls["port_reads"] == len(SIZES) + 4, calls
        assert calls["auth_changes"] == [2, 1, 0], calls
        assert calls["log_streams"] == 1, calls
        assert calls["service_changes"] == calls["mode_changes"] == 0 and calls["port_applies"] == 3, calls
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "application changed the terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: equal-height tabs, proxy review, controller apply, LAN access, numeric values, resource controls, preferences, resize, focus and logs; %s" % (language, profile, json.dumps(calls)), flush=True)
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
    scenario = parser.add_mutually_exclusive_group()
    scenario.add_argument("--tabs-only", action="store_true", help="Verify and capture tab spacing, row spacing, focus and hover")
    scenario.add_argument("--auth-only", action="store_true", help="Verify and capture the proxy authentication editor")
    scenario.add_argument("--modals-only", action="store_true", help="Verify and capture all three port and access dialogs")
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--lang", choices=["en", "zh-Hans"], action="append")
    parser.add_argument("--profile", choices=["truecolor", "256", "no-color"], action="append")
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("settings_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "256", "no-color"):
            exercise(preview, args.binary, output / language / profile, language, profile, args)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
