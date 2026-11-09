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
    # Button focus has a dedicated arrow gutter in every color capability.
    pattern = r"›\s*(?:[<\[]\s*)?" + re.escape(label) + r"(?:\s|[>\]]|$)"
    return any(re.search(pattern, row) for row in terminal.screen.text().splitlines())


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
    assert not any(value in terminal.screen.text() for value in "┌┐└┘│"), "inactive card retained an outer border"


def return_navigation(terminal, words):
    terminal.settle()
    wide = terminal.screen.columns >= 100 and terminal.screen.rows >= 20
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
    for y, row in enumerate(terminal.screen.cells):
        for x, value in enumerate(row):
            if value and "".join(row[x:]).startswith(label):
                return x, y
    return None


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


def inspect_row_cards(preview, terminal, output, width, height, words, profile, scenario, args):
    names = ("service", "profile", "route", "access", "traffic", "environment")
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
        if name == "traffic" and scenario in ("running", "pending", "drift"):
            for value in ("12.0 KiB/s", "2.2 MiB/s"):
                assert value in terminal.screen.text(), "traffic card omitted a live rate: " + value
        if name == "access":
            assert "127.0.0.1:17897" in terminal.screen.text(), "local-access card omitted its configured proxy endpoint"
            assert "127.0.0.1:19097" in terminal.screen.text(), "local-access card omitted its control API address"
            if scenario == "pending":
                assert words["tun.disable"] in terminal.screen.text(), "saved TUN state lost its explicit disable control"
                assert words["tun.saved_state"] % words["dashboard.control.on"] in terminal.screen.text(), "saved TUN state was not retained"
                assert words["dashboard.overview.tunActual"] % words["dashboard.control.off"] in terminal.screen.text(), "observed TUN state was confused with the saved state"
        preview.capture(terminal, output, "%s-card-%dx%d" % (name, width, height), args)
        if index + 1 < len(names):
            terminal.key(b"\x1b[B")
            terminal.settle()
    for first in (0, 2):
        assert abs(geometry[first]["width"] - geometry[first + 1]["width"]) <= 1, "paired cards have unequal column widths"
        left, right = initial_positions[first:first + 2]
        if left is not None and right is not None:
            assert left[1] == right[1], "paired card titles do not start on the same row"
    if scenario in ("running", "pending"):
        assert geometry[0]["height"] != geometry[1]["height"], "first-row cards were padded to equal heights"
    for first in (2, 4):
        before, current = initial_positions[first - 2], initial_positions[first]
        if before is not None and current is not None:
            expected = before[1] + max(geometry[first - 2]["height"], geometry[first - 1]["height"]) + 1
            assert current[1] == expected, "next card row does not follow the taller card and one blank row"
    for index in (4, 5):
        assert geometry[index]["width"] == width - 25, "traffic/environment card does not span the complete body"
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
        terminal.wait("v1.19.33" if scenario != "unavailable" else "Fixture service observation unavailable")
        terminal.settle()
        preview.capture(terminal, output, "overview-initial", args)
        screen = terminal.screen.text()
        if scenario in ("running", "pending"):
            for value in ("4242", "PROXY", "香港 01", "日常订阅"):
                assert value in screen, "missing initial observation: " + value
        elif scenario == "drift":
            for value in ("v1.20.0", "v1.19.33", "Fixture running service differs from saved settings"):
                assert value in screen, "missing state discrepancy: " + value
        elif scenario == "stopped":
            assert words["dashboard.action.start"] in screen, "stopped service lost Start"
        else:
            assert "12.0 KiB/s" not in screen, "unavailable observation retained live metrics"
        assert "subscription.example" in screen, "profile card omitted its source origin"
        if scenario == "pending":
            assert words["profiles.changes_pending"] in screen, "profile card hid its unapplied changes"

        # The first observation may already focus a card. Start explicitly from
        # navigation before checking both safe Enter transitions.
        return_navigation(terminal, words)
        assert_borderless(terminal)
        assert "─────" in terminal.screen.text(), "borderless cards removed the internal route divider"
        preview.capture(terminal, output, "overview-navigation", args)
        terminal.key(b"\r")
        terminal.settle()
        assert_card_outline(terminal, profile)
        preview.capture(terminal, output, "card-focus", args)
        terminal.key(b"\r")
        terminal.settle()
        lifecycle = words["dashboard.action.start" if scenario == "stopped" else "dashboard.action.stop"]
        assert focused(terminal, lifecycle), "entering the service card did not focus its first control"
        assert_borderless(terminal)
        preview.capture(terminal, output, "control-focus", args)

        expected = [lifecycle, words["dashboard.action.manage"], words["dashboard.action.profiles"], words["dashboard.settings.ports"]]
        if scenario != "unavailable":
            expected.extend([words["dashboard.action.restart"],
                             *[words["dashboard.mode." + mode] for mode in ("rule", "global", "direct")]])
        if scenario not in ("stopped", "unavailable"):
            expected.append(words["dashboard.action.openProxies"])
        if scenario == "pending":
            expected.append(words["tun.disable"])

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
            terminal.key(b"\r")
            terminal.settle()
            assert_borderless(terminal)
            assert focused(terminal, lifecycle), "card entry did not retain the first lifecycle control"
            preview.capture(terminal, output, "control-focus-%dx%d" % (width, height), args)
            tab_controls(terminal, expected)
            preview.capture(terminal, output, "tab-revealed-%dx%d" % (width, height), args)
            assert words["dashboard.footer.quit"] in terminal.screen.text().splitlines()[-1], "Quit hint left the footer"

        # Read-only page transitions use the card's real mouse target.
        focus_control(terminal, words["dashboard.action.profiles"])
        terminal.click_text(words["dashboard.action.profiles"])
        terminal.wait(words["dashboard.page.profiles"])
        terminal.wait_absent(words["dashboard.action.restart"])
        preview.capture(terminal, output, "profiles-from-overview", args)
        terminal.key(b"1")
        terminal.settle()
        if scenario not in ("stopped", "unavailable"):
            focus_control(terminal, words["dashboard.action.openProxies"])
            terminal.click_text(words["dashboard.action.openProxies"])
            terminal.wait("PROXY")
            terminal.wait_absent(words["dashboard.action.restart"])
            preview.capture(terminal, output, "proxies-from-overview", args)
            terminal.key(b"1")
            terminal.settle()
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["service_changes"] == calls["modes"] == calls["selections"] == 0, calls
        assert calls["tun_changes"] == 0, calls
        assert calls["runtime_queries"] > 0, calls
        check_profile(preview, terminal, profile)
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS %s/%s/%s: paired natural-height cards and fullwidth traffic/environment, five dimensions, borderless/focus states, local access, Tab reveal and read-only page actions; %s" %
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
    parser.add_argument("--scenario", choices=["running", "stopped", "unavailable", "drift", "pending"], action="append")
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
