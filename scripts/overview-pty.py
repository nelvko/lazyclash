#!/usr/bin/env python3
"""Verify Overview through a real controlling PTY with an in-memory backend.

Build with `go test -c ./internal/tui -o bin/overview-tui.test`. No real core,
managed data, network or service is used. Optional PNGs redraw captured VT
cells and colors; they are not native terminal-app screenshots.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys
import time
import uuid

from tui_layout import sidebar_visible


SIZES = ((160, 45), (120, 32), (80, 24), (60, 20), (40, 16), (160, 45))


def labels(language):
    result = {}
    directory = Path(__file__).resolve().parents[1] / "internal" / "i18n" / "locales"
    for path in directory.glob(language + ".*.json"):
        result.update(json.loads(path.read_text()))
    return result


def fixture_terminal(preview, binary, report, language, profile, scenario):
    environment = {"LAZYCLASH_OVERVIEW_PTY_REPORT": str(report),
                   "LAZYCLASH_OVERVIEW_PTY_LANGUAGE": language,
                   "LAZYCLASH_OVERVIEW_PTY_SCENARIO": scenario}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestOverviewPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def focused(terminal, label):
    pattern = r"›\s*(?:[<\[]\s*)?" + re.escape(label) + r"(?:\s|[>\]●]|$)"
    return any(re.search(pattern, row) for row in terminal.screen.text().splitlines())


def assert_core_affordance(terminal, words, scenario, profile, expect_focus=False):
    title = words["dashboard.overview.service"]
    x, y = text_point(terminal, title)
    cells = terminal.screen.cells
    manage = words["dashboard.core.manageAction"]
    manage_x, manage_y = text_point(terminal, manage)
    assert manage_y == y and manage_x > x, "Manage is not beside the core heading"
    right = cells[y].index("›", manage_x) + 1
    heading = "".join(cells[y][x:right]).removeprefix(title).strip()
    expected = (r"›\s+" if expect_focus else "") + re.escape(manage)
    assert re.fullmatch(expected, heading), "incorrect heading: " + repr(heading)
    if scenario == "unavailable":
        action = "—"
        status = "! " + words["dashboard.status.unknown"]
    elif scenario == "stopped":
        action = words["dashboard.settings.start"]
        status = "○ " + words["dashboard.status.stopped"]
    else:
        action = words["dashboard.settings.stop"]
        status = "● " + words["dashboard.status.running"]

    assert "".join(cells[y + 2][x:right]).strip() == "Mihomo", "missing core identity below heading"
    status_y = next((r for r in range(y + 3, len(cells)) if "".join(cells[r][x:right]).startswith(status)), None)
    assert status_y is not None, "missing core state: " + status
    action_text = "".join(cells[status_y][x:right]).removeprefix(status).strip()
    if profile == "no-color" and scenario != "unavailable":
        assert re.fullmatch(r"\[\s*" + re.escape(action) + r"\s*\]", action_text), action_text
        assert cells[status_y][right - 1] == "]", "button edge and Manage chevron are misaligned"
    else:
        assert action_text == action, (action_text, action)
        if scenario != "unavailable":
            backgrounds = [style[1] for style in terminal.screen.styles[status_y]]
            assert backgrounds[right - 1] != backgrounds[right] and backgrounds[right] == backgrounds[x], \
                "button background and Manage chevron are misaligned"

    size = (terminal.screen.columns, terminal.screen.rows)
    # With key hints shown the sidebar needs eleven rows for its menu and clock.
    spacious = sidebar_visible(*size) and terminal.screen.rows >= 11
    if spacious:
        assert all(cell == " " for cell in cells[status_y - 1][x:right]), "expected a blank row above the core state"
    else:
        assert status_y == y + 3, "compact state did not follow the core identity"
    if scenario in ("drift", "unavailable") and spacious:
        diagnostic = words["dashboard.core.restartVersion"] if scenario == "drift" else words["dashboard.core.doctorHint"]
        assert any(diagnostic in "".join(row[x:right]) for row in cells[y + 3:status_y - 1]), \
            "missing diagnostic above the state row: " + diagnostic

    uptime = words["dashboard.core.uptimeHoursMinutes"]
    clock_visible = sidebar_visible(*size) and any(
        re.fullmatch(r"[0-9]{2,}:[0-5][0-9]:[0-5][0-9]", "".join(row[:20]).strip()) for row in cells)
    running = scenario not in ("stopped", "unavailable")
    assert clock_visible == (running and spacious), "unexpected sidebar clock for %s at %s" % (scenario, size)
    body = ["".join(row[x:right]).strip() for row in cells[y + 1:status_y + 2]]
    if not running or clock_visible:
        prefix = uptime.split("%d", 1)[0].rstrip()
        assert all(prefix not in line for line in body), "core card displayed an unavailable or duplicate uptime"
    else:
        # The fixture starts at 2:18:34 and can cross a minute during resizing.
        assert body[-1] in (uptime % (2, 18), uptime % (2, 19)), "incorrect fallback uptime: " + repr(body)
    if scenario != "drift":
        assert all("v1.19.33" not in line for line in body), "core card exposed a version without a mismatch"
    return action


def inspect_core_management(preview, terminal, output, words, scenario, args, mouse):
    manage = words["dashboard.core.manageAction"]
    installed = words["wizard.installed_version"]
    version = "v1.20.0" if scenario == "drift" else "v1.19.33"
    if mouse:
        terminal.click_text(manage)
    else:
        assert focused(terminal, manage), "keyboard focus did not enter Manage"
        terminal.key(b"\r")
    terminal.wait(version + " ❯")
    terminal.settle()
    screen = terminal.screen.text()
    heading = text_point(terminal, words["settings.core"])
    field = text_point(terminal, installed)
    assert heading and field and heading[1] < field[1], "Core Management heading and installed version must be visible"
    row = screen.splitlines()[field[1]]
    assert version + " ❯" in row, "missing version link in Settings: " + row
    assert re.search(r"›\s*" + re.escape(installed), row), "Settings did not focus the installed version: " + row
    assert words["wizard.manage.title"] not in screen, "Manage opened version details before the row was activated"
    preview.capture(terminal, output, "core-management-%dx%d" % (terminal.screen.columns, terminal.screen.rows), args)
    if not mouse:
        terminal.key(b"\r")
        terminal.wait(words["wizard.manage.title"])
        terminal.wait(words["core_update.check"])
        terminal.key(b"\x1b")
        terminal.wait_absent(words["wizard.manage.title"])
        terminal.settle()
        assert focused(terminal, installed), "Closing version details did not return to the installed version"
    terminal.key(b"\x1b")
    terminal.wait(manage)
    terminal.settle()
    assert focused(terminal, manage), "Esc did not restore the Manage control"


def tab_controls(terminal, expected):
    seen = set()
    for _ in range(30):
        terminal.settle()
        seen.update(label for label in expected if focused(terminal, label))
        if seen == set(expected):
            return
        terminal.key(b"\t")
    raise AssertionError("Tab did not reveal controls: " + repr(set(expected) - seen) + "\n" + terminal.screen.text())


def focus_control(terminal, label):
    for _ in range(30):
        terminal.settle()
        if focused(terminal, label):
            return
        terminal.key(b"\t")
    raise AssertionError("Tab did not focus: " + label + "\n" + terminal.screen.text())


def check_profile(preview, terminal, profile):
    raw = bytes(terminal.raw)
    assert not preview.OSC_SET.search(raw), "application changed the terminal palette"
    if profile == "256":
        assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
    if profile == "no-color":
        assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"


def assert_borderless(terminal):
    assert not any(value in terminal.screen.text() for value in "┌┐└┘"), "inactive card retained an outer border"
    # The environment card retains an internal divider.
    for row in terminal.screen.cells:
        assert row.count("│") <= 1, "inactive card retained outer sides"


def return_navigation(terminal, words):
    terminal.settle()
    wide = sidebar_visible(terminal.screen.columns, terminal.screen.rows)
    prefix = words["navigation.sidebar" if wide else "navigation.menu"].split(" · ")[0]
    if terminal.screen.text().splitlines()[-1].lstrip().startswith(prefix):
        return
    terminal.key(b"\x1b")
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        terminal.read(0.05)
        if (not terminal.screen.synchronized and terminal.screen.state == "text"
                and terminal.screen.text().splitlines()[-1].lstrip().startswith(prefix)):
            return
    raise AssertionError("Esc did not restore navigation focus\n" + terminal.screen.text())


def text_point(terminal, label):
    return next(text_points(terminal, label), None)


def text_points(terminal, label):
    for y, row in enumerate(terminal.screen.cells):
        for x, value in enumerate(row):
            if value and "".join(row[x:]).startswith(label):
                yield x, y


def click_point(terminal, point):
    x, y = point
    terminal.key(f"\x1b[<0;{x+1};{y+1}M\x1b[<0;{x+1};{y+1}m".encode())


def check_network_modal(terminal, title, profile):
    cells = terminal.screen.cells
    if profile == "no-color":
        x, y = next((x, y) for x, y in text_points(terminal, title)
                    if y > 0 and "┌" in cells[y-1][:x] and "┐" in cells[y-1][x+1:])
        top = y - 1
        left = max(i for i in range(x) if cells[top][i] == "┌")
        right = next(i for i in range(x+1, len(cells[top])) if cells[top][i] == "┐")
        bottom = next(i for i in range(y+1, len(cells)) if cells[i][left] == "└" and cells[i][right] == "┘")
    else:
        styles = terminal.screen.styles
        x, y = next((x, y) for x, y in text_points(terminal, title)
                    if y > 0 and styles[y][x][1] != styles[y][0][1])
        top = y - 1
        surface = styles[y][x][1]
        columns = [i for i, cell in enumerate(styles[top]) if cell[1] == surface]
        left, right = min(columns), max(columns)
        bottom = next(i for i in range(y+1, len(cells)) if styles[i][left][1] != surface) - 1
        for row in (cells[top], cells[bottom]):
            assert all(cell == " " for cell in row[left:right+1]), (title, row)
    margins = left, len(cells[top])-1-right, top, len(cells)-1-bottom
    assert min(margins[:2]) >= 2 and min(margins[2:]) >= 1, (title, margins)
    assert abs(margins[0]-margins[1]) <= 1 and abs(margins[2]-margins[3]) <= 1, (title, margins)
    return left, top, right, bottom


def inspect_network_editors(preview, terminal, output, words, profile, scenario, args):
    edit = words["dashboard.network.configure"]
    if scenario == "pending":
        primary, tun = words["terminal.title"], words["dashboard.network.tun"]
        focus_control(terminal, "○ " + words["dashboard.network.off"])
        saved = "● " + words["tun.saved_state"] % words["dashboard.control.on"]
        for title, label in ((primary, edit), (tun, saved), (tun, edit)):
            terminal.key(b"\t")
            terminal.settle()
            x, y = terminal.screen.setting_control(title, label)
            assert "›" in terminal.screen.cells[y][max(0, x-3):x], "Tab did not reach " + title + " / " + label
    for section, heading, title in (
            ("terminal", words["terminal.title"], words["network.terminalConfig.title"]),
            ("tun", words["dashboard.network.tun"], words["network.tunConfig.title"])):
        if section == "tun":
            terminal.resize(40, 16)
            terminal.settle()
            # Tab scrolls the next setting into the short viewport.
            terminal.key(b"\t")
        cancel = words["profiles.close"] if section == "tun" and scenario != "pending" else words["ports.cancel"]
        focus_control(terminal, edit)
        point = terminal.screen.setting_control(heading, edit)
        assert point is not None, "network edit action missing for " + heading
        click_point(terminal, point)
        terminal.wait(title)
        terminal.wait(cancel)
        if section == "terminal":
            terminal.wait("bash")
            terminal.wait(words["network.terminalConfig.save"])
            terminal.wait("127.0.0.1:17897")
            for hidden in ("19097", "private-user", "private-password", "private-path", "private-token", "private-fragment"):
                assert hidden not in terminal.screen.text(), "terminal editor leaked controller details: " + hidden
        for w, h in ((160, 45), (80, 24), (40, 16)):
            terminal.resize(w, h)
            terminal.settle()
            terminal.wait(title)
            check_network_modal(terminal, title, profile)
            # Clicking the backdrop and paging the viewport leave the modal in control.
            click_point(terminal, (0, 0))
            terminal.key(b"\x1b[6~")
            terminal.wait(title)
            terminal.wait(cancel)
            preview.capture(terminal, output, f"{section}-config-{w}x{h}", args)
        terminal.resize(160, 45)
        terminal.settle()
        if section == "tun" and scenario == "pending":
            terminal.click_text("[x]")
            terminal.wait("[ ]")
        if section == "tun":
            # The backdrop can contain the same Chinese word in a status label.
            left, top, right, bottom = check_network_modal(terminal, title, profile)
            point = next((x, y) for x, y in text_points(terminal, cancel) if left < x < right and top < y < bottom)
            click_point(terminal, point)
        else:
            terminal.key(b"\x1b")
        terminal.wait_absent(title)
        terminal.settle()
        # Keyboard activation reopens the editor that the mouse entered.
        terminal.key(b"\r")
        terminal.wait(title)
        terminal.wait(cancel)
        terminal.key(b"\x1b")
        terminal.settle()


def subscription_allowance(terminal):
    balance = "7.0 GiB / 10.0 GiB"
    point = text_point(terminal, balance)
    assert point is not None, "subscription card omitted its remaining and total quota"
    x, y = point
    row = "".join(terminal.screen.cells[y][x:])
    expiry = re.search(r"\d{4}-\d{2}-\d{2}", row)
    assert expiry, "subscription balance row omitted the expiry: " + row
    assert terminal.screen.cells[y + 1][x] == "━", "subscription card omitted the remaining quota bar"
    return expiry.group()


def assert_card_outline(terminal, profile, complete=True):
    cells = terminal.screen.cells
    corners = [(x, y) for y, row in enumerate(cells) for x, value in enumerate(row) if value == "┌"]
    assert len(corners) == 1, "card focus did not draw exactly one top-left corner"
    left, top = corners[0]
    right = next((x for x in range(left + 1, len(cells[top])) if cells[top][x] == "┐"), None)
    assert right is not None, "card focus omitted its top-right corner"
    assert all(value == "─" for value in cells[top][left + 1:right]), "card focus has an incomplete top edge"
    bottom = next((y for y in range(top + 1, len(cells)) if cells[y][left] == "└"), None)
    if complete:
        assert bottom is not None, "card focus omitted its bottom-left corner"
    if bottom is not None:
        assert cells[bottom][right] == "┘", "card focus omitted its bottom-right corner"
        assert all(value == "─" for value in cells[bottom][left + 1:right]), "card focus has an incomplete bottom edge"
        assert all(cells[y][left] == cells[y][right] == "│" for y in range(top + 1, bottom)), "card focus has incomplete side edges"
    if profile != "no-color":
        color = terminal.screen.styles[top][left][0]
        if isinstance(color, str):
            color = tuple(int(color[index:index + 2], 16) for index in (1, 3, 5))
        assert color[0] > 190 and color[0] >= color[1] > color[2] and color[1] >= 110, "card outline lost its yellow focus color"
    return {"x": left, "y": top, "width": right - left + 1,
            "height": bottom - top + 1 if bottom is not None else None}


def inspect_network_card(terminal, outline, words, scenario):
    x, y = outline["x"], outline["y"]
    right, bottom = x + outline["width"], y + outline["height"]
    card_rows = ["".join(row[x:right]) for row in terminal.screen.cells[y:bottom]]
    card_text = "\n".join(card_rows)
    edit = words["dashboard.network.configure"]
    assert card_text.count(edit) == 2, "both network sections need their own editor"
    for hidden in ("127.0.0.1:", "17897", words["dashboard.field.systemProxy"]):
        assert hidden not in card_text, "network summary retained " + hidden
    terminal_title, tun_title = words["terminal.title"], words["dashboard.network.tun"]
    terminal_x, terminal_y = text_point(terminal, terminal_title)
    tun_x, tun_y = text_point(terminal, tun_title)
    assert x < terminal_x == tun_x < right and y < terminal_y < tun_y < bottom, "settings must line up down the card"
    assert not "".join(terminal.screen.cells[tun_y-1][x+1:right-1]).strip(), "missing blank row between settings"
    terminal_state = "○ " + words["dashboard.network.off"]
    tun_state = ("● " + words["tun.saved_state"] % words["dashboard.control.on"]
                 if scenario == "pending" else words["dashboard.network.unsupported"])
    columns = []
    for title, title_y, end_y, state in ((terminal_title, terminal_y, tun_y-1, terminal_state),
                                          (tun_title, tun_y, bottom-1, tun_state)):
        state_at = terminal.screen.setting_control(title, state)
        edit_at = terminal.screen.setting_control(title, edit)
        assert state_at and edit_at, (title, state, edit)
        state_x, state_y = state_at
        edit_x, edit_y = edit_at
        assert title_y <= state_y <= edit_y < end_y and x < state_x < right and x < edit_x < right, (state_at, edit_at)
        if outline["width"] >= 60:
            assert state_y == edit_y == title_y, "wide setting must fit on one row: " + title
        if title_y == state_y:
            assert terminal_x < state_x, "state must follow the setting name: " + title
        if state_y == edit_y:
            assert state_x < edit_x, "configuration must follow the setting state: " + title
        columns.append((state_x, edit_x))
    assert columns[0] == columns[1], "the two settings must align their states and configuration controls"
    if scenario == "pending":
        tun_text = " ".join("".join(row[x+1:right-1]).strip() for row in terminal.screen.cells[tun_y+1:bottom-1])
        assert words["dashboard.overview.tunActual"] % words["dashboard.control.off"] in tun_text, "actual state missing beneath TUN"


def inspect_row_cards(preview, terminal, output, width, height, words, profile, scenario, args):
    names = ("service", "profile", "network", "route", "environment", "ipInfo", "traffic")
    titles = [words["dashboard.profile.title"] if name == "profile" else
              words["dashboard.overview." + name] for name in names]
    initial_positions = [text_point(terminal, title) for title in titles]
    if profile != "no-color" and all(initial_positions[i] for i in (2, 3, 5)):
        network_x, network_y = initial_positions[2]
        route_x, route_y = initial_positions[3]
        ip_y = initial_positions[5][1]
        backgrounds = terminal.screen.styles
        panel = backgrounds[route_y][route_x][1]
        assert all(backgrounds[row][route_x][1] == panel for row in range(route_y, ip_y + 1)), \
            "routing and IP panels have a break in their background"
        assert any(backgrounds[row][network_x][1] != panel for row in range(network_y, ip_y)), \
            "network and environment panels lost their separation"
    geometry = []
    for index, (name, title) in enumerate(zip(names, titles)):
        outline = assert_card_outline(terminal, profile)
        point = text_point(terminal, title)
        assert point is not None and outline["x"] < point[0] < outline["x"] + outline["width"], "card browsing lost semantic identity: " + name
        assert outline["y"] < point[1] < outline["y"] + outline["height"], "card outline surrounds the wrong title: " + name
        geometry.append(outline)
        if name == "traffic" and scenario in ("running", "pending", "drift", "direct"):
            for value in ("12.0 KiB/s", "2.2 MiB/s"):
                assert value in terminal.screen.text(), "traffic card omitted a live rate: " + value
        if name == "network":
            inspect_network_card(terminal, outline, words, scenario)
        if name == "route":
            x, y = outline["x"] + 1, outline["y"] + 1
            right, bottom = x + outline["width"] - 2, y + outline["height"] - 2
            lines = ["".join(row[x:right]).strip() for row in terminal.screen.cells[y:bottom]]
            path = {"running": "AUTO ▾ → 新加坡 02", "pending": "PROXY → 香港 01 · 日常线路",
                    "drift": "GLOBAL → PROXY → 香港 01 · 日常线路"}.get(scenario)
            if path:
                assert path in " ".join(" ".join(lines).split()), "incorrect routing path: " + repr(lines)
                if scenario in ("running", "pending"):
                    modes = next(i for i, line in enumerate(lines) if words["dashboard.mode.global"] in line)
                    assert not lines[modes + 1] and path == " ".join(lines[modes + 2].split()), \
                        "expected a blank line and the path directly beneath the modes: " + repr(lines)
            else:
                assert all("香港 01" not in line for line in lines), "unobserved member in routing card"
        if name == "environment":
            x, y = outline["x"], outline["y"]
            right, bottom = x + outline["width"], y + outline["height"]
            card_text = "\n".join("".join(row[x:right]) for row in terminal.screen.cells[y:bottom])
            for key in ("dashboard.field.platform", "dashboard.overview.serviceLabel", "settings.autostart"):
                assert words[key] in card_text, "environment card omitted " + words[key]
            assert (words["dashboard.overview.linger"] in card_text) == (scenario != "pending"), "environment card showed the wrong linger fields for its scope"
        if name == "ipInfo":
            if scenario in ("stopped", "unavailable"):
                assert words["dashboard.ip.unavailable"] in terminal.screen.text(), "missing unavailable IP state"
            else:
                assert_fixture_ip(terminal, words)
        preview.capture(terminal, output, "%s-card-%dx%d" % (name, width, height), args)
        if index + 1 < len(names):
            terminal.key(b"\x1b[B")
            terminal.settle()
    for first in (0, 2, 4):
        assert geometry[first]["width"] == geometry[first + 1]["width"], "paired cards have unequal column widths"
        assert geometry[first]["height"] == geometry[first + 1]["height"], "paired cards have unequal heights"
        left, right = initial_positions[first:first + 2]
        if left is not None and right is not None:
            assert left[1] == right[1], "paired card titles do not start on the same row"
    for first in (2, 4, 6):
        before, current = initial_positions[first - 2], initial_positions[first]
        if before is not None and current is not None:
            expected = before[1] + max(geometry[first - 2]["height"], geometry[first - 1]["height"]) + 1
            assert current[1] == expected, "next card row does not follow the taller card and one blank row"
    left, right, environment, ip, traffic = geometry[0], geometry[1], *geometry[4:]
    paired_width = right["x"] + right["width"] - left["x"]
    assert traffic["x"] == left["x"] and traffic["width"] in (paired_width, paired_width + 1), "traffic card does not span the paired columns"
    assert environment["x"] == left["x"] and environment["width"] == left["width"], "environment card does not align with the left column"
    assert ip["x"] == right["x"] and ip["y"] == environment["y"], "IP card does not align with the right column beside the environment"
    (output / ("card-geometry-%dx%d.json" % (width, height))).write_text(json.dumps(dict(zip(names, geometry)), indent=2) + "\n")
    return_navigation(terminal, words)
    terminal.key(b"\t")
    terminal.settle()


def assert_fixture_ip(terminal, words):
    screen = terminal.screen.text()
    for value in ("203.0.113.17", "Japan", "Tokyo", "Example ISP", "AS64496"):
        assert value in screen, "missing IP information: " + value
    title_y = text_point(terminal, words["dashboard.overview.ipInfo"])[1]
    age = re.escape(words["profiles.updated_minutes"]).replace("%d", r"\d+")
    stamp = re.escape(words["profiles.updated_time"]).replace("%s", "(?:" + re.escape(words["profiles.just_updated"]) + "|" + age + ")")
    assert any(re.search(stamp, row) and words["header.refresh"] in row
               for row in screen.splitlines()[title_y:title_y+2]), \
        "IP heading omitted its update time or refresh action"


def inspect_route_picker(preview, terminal, output, words, args):
    for width, height in ((160, 45), (40, 16)):
        terminal.resize(width, height)
        terminal.settle()
        focus_control(terminal, "AUTO")
        terminal.key(b"\r")
        terminal.wait("负载均衡")
        assert "GLOBAL" not in terminal.screen.text(), "global group offered in Rule mode"
        preview.capture(terminal, output, f"group-menu-{width}x{height}", args)
        terminal.key(b"\x1b[B")  # PROXY follows AUTO in the Rule-mode menu.
        terminal.key(b"\r")
        terminal.wait_absent("负载均衡")
        terminal.wait("香港 01")
        terminal.click_text("▾")
        terminal.wait("办公")
        terminal.click_text("办公")
        terminal.wait_absent("负载均衡")
        terminal.wait("新加坡 02")
        screen = terminal.screen.text()
        assert re.search(r"办公\s*▾\s*→\s*AUTO\s*→\s*新加坡 02", screen), screen
        preview.capture(terminal, output, f"group-path-{width}x{height}", args)

        terminal.click_text("▾")
        terminal.wait("负载均衡")
        terminal.click_text("负载均衡")
        terminal.wait(words["proxies.distributed"])
        terminal.key(b"\r")
        terminal.wait("办公")
        terminal.click_text("AUTO")
        terminal.wait_absent("负载均衡")
        terminal.wait("新加坡 02")
    terminal.resize(160, 45)
    terminal.settle()


def exercise_ip_route(preview, terminal, output, words, report, args):
    terminal.wait("203.0.113.17")
    terminal.key(b"2")
    terminal.wait(words["providers.title.proxies"])
    terminal.click_text(words["dashboard.mode.direct"])
    terminal.wait(words["proxies.direct.description"])
    terminal.click_text(words["dashboard.mode.global"])
    terminal.wait("GLOBAL")

    # The return to Overview starts one lookup for Global. Change the actual
    # mode again while that response is held back by the fixture.
    terminal.key(b"1")
    terminal.wait(words["dashboard.ip.loading"])
    terminal.click_text(words["dashboard.mode.direct"])
    terminal.wait("203.0.113.18")
    terminal.settle()
    assert "203.0.113.18" in terminal.screen.text() and "203.0.113.99" not in terminal.screen.text(), terminal.screen.text()
    preview.capture(terminal, output, "ip-latest-route", args)
    terminal.exit()
    calls = json.loads(report.read_text())
    assert calls["ip_lookups"] == 3 and calls["modes"] == 3 and calls["selections"] == 0, calls
    print("PASS IP route changes: one initial lookup, deferred off Overview, latest selection displayed", flush=True)


def exercise(preview, binary, output, language, profile, scenario, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile, scenario)
    try:
        if scenario == "ip-route":
            exercise_ip_route(preview, terminal, output, words, report, args)
            return
        terminal.wait("subscription.example")
        terminal.settle()
        preview.capture(terminal, output, "overview-initial", args)
        screen = terminal.screen.text()
        lifecycle = assert_core_affordance(terminal, words, scenario, profile)
        if scenario in ("running", "pending"):
            for value in ("新加坡 02" if scenario == "running" else "香港 01", "日常订阅"):
                assert value in screen, "missing initial observation: " + value
            assert "4242" not in screen, "core status retained diagnostic PID"
        elif scenario == "drift":
            for value in (words["dashboard.kernel.installed"] % "v1.20.0",
                          words["dashboard.kernel.running"] % "v1.19.33", words["dashboard.core.restartVersion"]):
                assert value in screen, "missing state discrepancy: " + value
        elif scenario == "direct":
            assert words["dashboard.overview.modeDirect"] in screen, "actual Direct omitted its explanation"
            assert "香港 01" not in screen, "Direct retained a stale selected node"
        else:
            assert "12.0 KiB/s" not in screen, "unavailable observation retained live metrics"
        profile_name = text_point(terminal, "日常订阅 · 工作与生活.yaml")
        source = text_point(terminal, "subscription.example")
        assert profile_name and source, "profile card omitted the applied name or its source"
        assert source[1] == profile_name[1] + (2 if scenario in ("pending", "unavailable") else 1), \
            "unexpected status rows between the applied name and source"
        expiry = subscription_allowance(terminal)
        assert "17897" not in screen, "the saved mixed port belongs in its editor"
        assert "127.0.0.1:" not in screen, "overview retained addresses intended for network details"
        if scenario == "pending":
            assert words["profiles.refresh_failed"] in screen, "profile card hid its unapplied changes"

        if scenario == "running":
            terminal.resize(120, 10)
            terminal.settle()
            assert_core_affordance(terminal, words, scenario, profile)
            preview.capture(terminal, output, "short-sidebar-card-uptime", args)
            terminal.resize(160, 45)
            terminal.settle()

        # The first observation may already focus a card. Start explicitly from
        # navigation before checking both safe Enter transitions.
        return_navigation(terminal, words)
        assert_borderless(terminal)
        preview.capture(terminal, output, "overview-navigation", args)
        terminal.key(b"\r")
        terminal.settle()
        assert_card_outline(terminal, profile)
        preview.capture(terminal, output, "card-focus", args)
        terminal.key(b"\r")
        terminal.settle()
        assert_core_affordance(terminal, words, scenario, profile, expect_focus=True)
        assert_borderless(terminal)
        preview.capture(terminal, output, "control-focus", args)

        inspect_core_management(preview, terminal, output, words, scenario, args, mouse=False)

        expected = [words["dashboard.core.manageAction"], lifecycle, words["dashboard.overview.profilesAction"], words["dashboard.network.configure"], words["dashboard.overview.proxiesAction"]]
        if scenario != "unavailable":
            expected.extend([words["dashboard.mode." + mode] for mode in ("rule", "global", "direct")])

        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            terminal.key(b"\x1b[H")
            terminal.settle()
            preview.capture(terminal, output, "overview-%dx%d" % (width, height), args)
            return_navigation(terminal, words)
            assert_borderless(terminal)
            preview.capture(terminal, output, "navigation-%dx%d" % (width, height), args)
            # Navigation's next Tab stop is the safe card-browsing surface.
            terminal.key(b"\t")
            terminal.settle()
            assert_card_outline(terminal, profile, complete=scenario == "running" or height >= 32)
            preview.capture(terminal, output, "card-focus-%dx%d" % (width, height), args)
            if width >= 120:
                inspect_row_cards(preview, terminal, output, width, height, words, profile, scenario, args)
            elif width in (80, 40):
                for _ in range(2):
                    terminal.key(b"\x1b[B")
                    terminal.settle()
                outline = assert_card_outline(terminal, profile)
                inspect_network_card(terminal, outline, words, scenario)
                preview.capture(terminal, output, "network-card-%dx%d" % (width, height), args)
                if width == 80 and scenario == "running":
                    for name in ("route", "environment", "ipInfo", "traffic"):
                        terminal.key(b"\x1b[B")
                        terminal.settle()
                        x, y = text_point(terminal, words["dashboard.overview." + name])
                        card = assert_card_outline(terminal, profile)
                        assert card["y"] < y < card["y"] + card["height"] and card["x"] < x, name
                        assert (card["x"], card["width"]) == (outline["x"], outline["width"]), name
                        if name == "ipInfo":
                            assert_fixture_ip(terminal, words)
                            preview.capture(terminal, output, "ipInfo-card-%dx%d" % (width, height), args)
                return_navigation(terminal, words)
                terminal.key(b"\t")
                terminal.settle()
            terminal.key(b"\r")
            terminal.settle()
            assert_borderless(terminal)
            assert_core_affordance(terminal, words, scenario, profile, expect_focus=True)
            preview.capture(terminal, output, "control-focus-%dx%d" % (width, height), args)
            if width == 80:
                inspect_core_management(preview, terminal, output, words, scenario, args, mouse=True)
            terminal.key(b"\t")
            terminal.settle()
            assert focused(terminal, lifecycle), "Tab from Manage did not focus the lifecycle action"
            tab_controls(terminal, expected)
            preview.capture(terminal, output, "tab-revealed-%dx%d" % (width, height), args)
            assert words["dashboard.footer.quit"] in terminal.screen.text().splitlines()[-1], "Quit hint left the footer"

        if scenario == "running":
            refresh = words["header.refresh"]
            focus_control(terminal, refresh)
            terminal.key(b"\r")
            terminal.wait(words["dashboard.ip.failed"])
            assert focused(terminal, refresh), "failed IP lookup lost the refresh control"
            terminal.key(b"\r")
            terminal.wait("203.0.113.18")
            assert focused(terminal, refresh), "successful IP retry lost the refresh control"
            preview.capture(terminal, output, "ipInfo-refreshed", args)
            inspect_route_picker(preview, terminal, output, words, args)
        inspect_network_editors(preview, terminal, output, words, profile, scenario, args)

        # Read-only page transitions use the card's real mouse target.
        focus_control(terminal, words["dashboard.overview.profilesAction"])
        terminal.click_text(words["dashboard.overview.profilesAction"])
        terminal.wait(words["dashboard.page.profiles"])
        terminal.wait_absent(words["dashboard.action.restart"])
        preview.capture(terminal, output, "profiles-from-overview", args)
        assert subscription_allowance(terminal) == expiry, "subscription expiry differs between pages"
        terminal.key(b"1")
        terminal.settle()
        if scenario not in ("stopped", "unavailable"):
            focus_control(terminal, words["dashboard.overview.proxiesAction"])
            terminal.click_text(words["dashboard.overview.proxiesAction"])
            expected_proxy = words["proxies.direct.description"] if scenario == "direct" else "GLOBAL" if scenario == "drift" else "PROXY"
            terminal.wait(expected_proxy)
            terminal.wait_absent(words["dashboard.action.restart"])
            preview.capture(terminal, output, "proxies-from-overview", args)
            terminal.key(b"1")
            terminal.settle()
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["service_changes"] == calls["modes"] == calls["selections"] == 0, calls
        assert calls["tun_changes"] == calls["terminal_changes"] == 0, calls
        assert calls["settings_reads"] == 2 and calls["settings_writes"] == 0, calls
        assert calls["runtime_queries"] > 0, calls
        if scenario in ("stopped", "unavailable"):
            assert calls["ip_lookups"] == 0, calls
        elif scenario == "running":
            assert calls["ip_lookups"] == 3, calls
        else:
            assert calls["ip_lookups"] == 1, calls
        check_profile(preview, terminal, profile)
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS %s/%s/%s: responsive Overview, core management and focus, terminal and TUN editors, draft cancellation; %s" %
              (language, profile, scenario, json.dumps(calls)), flush=True)
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
    parser.add_argument("--scenario", choices=["running", "stopped", "unavailable", "drift", "pending", "direct", "ip-route"], action="append")
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("overview_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "256", "no-color"):
            for scenario in args.scenario or ("running",):
                exercise(preview, args.binary, output / language / profile / scenario, language, profile, scenario, args)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
