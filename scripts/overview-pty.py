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
    pattern = r"›\s*(?:[<\[]\s*)?" + re.escape(label) + r"(?:\s|[>\]]|$)"
    return any(re.search(pattern, row) for row in terminal.screen.text().splitlines())


def assert_core_affordance(terminal, words, scenario, profile, expect_focus=False):
    title = words["dashboard.overview.service"]
    x, y = text_point(terminal, title)
    neighbor = text_point(terminal, words["dashboard.profile.title"])
    right = neighbor[0] if neighbor and neighbor[1] == y else terminal.screen.columns
    heading = "".join(terminal.screen.cells[y][x:right]).removeprefix(title)
    if scenario == "unavailable":
        action = "—"
        status = "! " + words["dashboard.status.unknown"]
    elif scenario == "stopped":
        action = words["dashboard.settings.start"]
        status = "○ " + words["dashboard.status.stopped"]
    else:
        action = words["dashboard.settings.stop"]
        status = "● " + words["dashboard.status.running"]
    face = re.escape(action)
    if profile == "no-color" and scenario != "unavailable":
        face = r"\[\s*" + face + r"\s*\]"
    marker = "›" if expect_focus else ""
    assert re.fullmatch(r"\s*" + marker + r"\s*" + face + r"[\s│]*", heading), \
        "incorrect heading action or focus: " + repr(heading)
    cells = terminal.screen.cells
    body = ["".join(row[x:right]).strip(" │") for row in cells[y + 1:y + 8]]
    assert body[1:3] == ["Mihomo", status], "incorrect core name or observation: " + repr(body)
    uptime = words["dashboard.core.uptimeHoursMinutes"]
    size = (terminal.screen.columns, terminal.screen.rows)
    clock_visible = sidebar_visible(*size) and any(
        re.fullmatch(r"[0-9]{2,}:[0-5][0-9]:[0-5][0-9]", "".join(row[:20]).strip()) for row in cells)
    running = scenario not in ("stopped", "unavailable")
    if running and size in ((160, 45), (120, 32)):
        assert clock_visible, "wide sidebar omitted the running process clock"
    if size == (120, 10):
        assert not clock_visible, "short sidebar retained the clock ahead of the navigation"
    if not running or clock_visible:
        prefix = uptime.split("%d", 1)[0].rstrip()
        assert all(prefix not in line for line in body), "core card displayed an unavailable or duplicate uptime"
    else:
        # The fixture starts at 2:18:34 and can cross a minute during resizing.
        assert body[3] in (uptime % (2, 18), uptime % (2, 19)), "incorrect fallback uptime: " + repr(body)
    if scenario != "drift":
        assert all("v1.19.33" not in line for line in body), "core card exposed a version without a mismatch"
    return action


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
    # The network card can have one internal vertical divider.
    edges = {x for row in terminal.screen.cells for x, value in enumerate(row) if value == "│"}
    assert len(edges) <= 1, "inactive card retained outer sides"


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


def check_network_modal(terminal, title):
    """Find the frame enclosing its title; input fields have their own frames."""
    cells = terminal.screen.cells
    x, y = next((x, y) for x, y in text_points(terminal, title)
                if y > 0 and "┌" in cells[y-1][:x] and "┐" in cells[y-1][x+1:])
    top = y - 1
    left = max(i for i in range(x) if cells[top][i] == "┌")
    right = next(i for i in range(x+1, len(cells[top])) if cells[top][i] == "┐")
    bottom = next(i for i in range(y+1, len(cells)) if cells[i][left] == "└" and cells[i][right] == "┘")
    margins = left, len(cells[top])-1-right, top, len(cells)-1-bottom
    assert min(margins[:2]) >= 2 and min(margins[2:]) >= 1, (title, margins)
    assert abs(margins[0]-margins[1]) <= 1 and abs(margins[2]-margins[3]) <= 1, (title, margins)


def inspect_network_editors(preview, terminal, output, words, scenario, args):
    edit = words["dashboard.network.configure"]
    if scenario == "pending":
        primary, tun = words["terminal.title"], words["dashboard.network.tun"]
        focus_control(terminal, "○ " + words["dashboard.network.off"])
        saved = "● " + words["tun.saved_state"] % words["dashboard.control.on"]
        for title, offset, label in ((primary, 2, edit), (tun, 1, saved), (tun, 2, edit)):
            terminal.key(b"\t")
            terminal.settle()
            x, y = terminal.screen.point_below(title, offset, label)
            assert "›" in terminal.screen.cells[y][max(0, x-3):x], "Tab did not reach " + title + " / " + label
    for section, heading, title in (
            ("terminal", words["terminal.title"], words["network.terminalConfig.title"]),
            ("tun", words["dashboard.network.tun"], words["network.tunConfig.title"])):
        cancel = words["profiles.close"] if section == "tun" and scenario != "pending" else words["ports.cancel"]
        focus_control(terminal, edit)
        point = terminal.screen.point_below(heading, 2, edit)
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
            check_network_modal(terminal, title)
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
            terminal.click_text(cancel)
        else:
            terminal.key(b"\x1b")
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
    left_x, left_y = text_point(terminal, terminal_title)
    tun_x, tun_y = text_point(terminal, tun_title)
    first_col, end_col = x + 2, right - 2
    if outline["width"] >= 44:
        assert left_y == tun_y and x < left_x < tun_x < right, "network groups are not side by side"
        split = terminal.screen.cells[left_y].index("│", left_x, tun_x)
        sections = ((terminal_title, left_y, first_col, split-1),
                    (tun_title, tun_y, split+2, end_col))
    else:
        assert y < left_y < tun_y < bottom, "narrow network groups are not stacked"
        sections = ((terminal_title, left_y, first_col, end_col),
                    (tun_title, tun_y, first_col, end_col))
    for title, title_y, start, end in sections:
        for offset in range(3):  # Heading, switch and configuration button.
            cells = terminal.screen.cells[title_y+offset]
            ink = [col for col in range(start, end) if cells[col] != " "]
            assert ink, "empty network section row under " + title
            margins = ink[0] - start, end - 1 - ink[-1]
            assert abs(margins[0]-margins[1]) <= 1, (title, offset, margins)
    assert terminal.screen.point_below(terminal_title, 1, "○ " + words["dashboard.network.off"]), "missing terminal proxy switch"
    tun_state = ("● " + words["tun.saved_state"] % words["dashboard.control.on"]
                 if scenario == "pending" else words["dashboard.network.unsupported"])
    assert terminal.screen.point_below(tun_title, 1, tun_state), "incorrect state beneath TUN"
    for title in (terminal_title, tun_title):
        assert terminal.screen.point_below(title, 2, edit), "no configuration button beneath " + title
    if scenario == "pending":
        _, _, start, end = sections[1]
        tun_text = " ".join("".join(row[start:end]).strip() for row in terminal.screen.cells[tun_y:bottom-1])
        assert words["dashboard.overview.tunActual"] % words["dashboard.control.off"] in tun_text, "actual state missing beneath TUN"


def inspect_row_cards(preview, terminal, output, width, height, words, profile, scenario, args):
    names = ("service", "profile", "network", "route", "traffic", "environment")
    titles = [words["dashboard.profile.title"] if name == "profile" else
              words["dashboard.overview." + name] for name in names]
    initial_positions = [text_point(terminal, title) for title in titles]
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
        if name == "environment":
            x, y = outline["x"], outline["y"]
            right, bottom = x + outline["width"], y + outline["height"]
            card_text = "\n".join("".join(row[x:right]) for row in terminal.screen.cells[y:bottom])
            for key in ("dashboard.field.platform", "dashboard.overview.serviceLabel", "settings.autostart"):
                assert words[key] in card_text, "environment card omitted " + words[key]
            assert (words["dashboard.overview.linger"] in card_text) == (scenario != "pending"), "environment card showed the wrong linger fields for its scope"
        preview.capture(terminal, output, "%s-card-%dx%d" % (name, width, height), args)
        if index + 1 < len(names):
            terminal.key(b"\x1b[B")
            terminal.settle()
    for first in (0, 2):
        assert geometry[first]["width"] == geometry[first + 1]["width"], "paired cards have unequal column widths"
        assert geometry[first]["height"] == geometry[first + 1]["height"], "paired cards have unequal heights"
        left, right = initial_positions[first:first + 2]
        if left is not None and right is not None:
            assert left[1] == right[1], "paired card titles do not start on the same row"
    for first in (2, 4):
        before, current = initial_positions[first - 2], initial_positions[first]
        if before is not None and current is not None:
            expected = before[1] + max(geometry[first - 2]["height"], geometry[first - 1]["height"]) + 1
            assert current[1] == expected, "next card row does not follow the taller card and one blank row"
    left, right, traffic, environment = geometry[0], geometry[1], geometry[4], geometry[5]
    paired_width = right["x"] + right["width"] - left["x"]
    assert traffic["x"] == left["x"] and traffic["width"] in (paired_width, paired_width + 1), "traffic card does not span the paired columns"
    assert environment["x"] == left["x"] and environment["width"] == left["width"], "environment card does not align with the left column"
    if initial_positions[4] is not None and initial_positions[5] is not None:
        assert initial_positions[5][1] == initial_positions[4][1] + geometry[4]["height"] + 1, "environment does not follow the fullwidth traffic card"
    (output / ("card-geometry-%dx%d.json" % (width, height))).write_text(json.dumps(dict(zip(names, geometry)), indent=2) + "\n")
    return_navigation(terminal, words)
    terminal.key(b"\t")
    terminal.settle()


def exercise(preview, binary, output, language, profile, scenario, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile, scenario)
    try:
        terminal.wait("subscription.example")
        terminal.settle()
        preview.capture(terminal, output, "overview-initial", args)
        screen = terminal.screen.text()
        lifecycle = assert_core_affordance(terminal, words, scenario, profile)
        if scenario in ("running", "pending"):
            for value in ("香港 01", "日常订阅"):
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

        expected = [lifecycle, words["dashboard.overview.profilesAction"], words["dashboard.network.configure"], words["dashboard.overview.proxiesAction"]]
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
                inspect_network_card(terminal, assert_card_outline(terminal, profile), words, scenario)
                preview.capture(terminal, output, "network-card-%dx%d" % (width, height), args)
                return_navigation(terminal, words)
                terminal.key(b"\t")
                terminal.settle()
            terminal.key(b"\r")
            terminal.settle()
            assert_borderless(terminal)
            assert_core_affordance(terminal, words, scenario, profile, expect_focus=True)
            preview.capture(terminal, output, "control-focus-%dx%d" % (width, height), args)
            tab_controls(terminal, expected)
            preview.capture(terminal, output, "tab-revealed-%dx%d" % (width, height), args)
            assert words["dashboard.footer.quit"] in terminal.screen.text().splitlines()[-1], "Quit hint left the footer"

        inspect_network_editors(preview, terminal, output, words, scenario, args)

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
        check_profile(preview, terminal, profile)
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS %s/%s/%s: responsive Overview, centered terminal and TUN editors, keyboard and mouse, draft cancellation; %s" %
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
    parser.add_argument("--scenario", choices=["running", "stopped", "unavailable", "drift", "pending", "direct"], action="append")
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
