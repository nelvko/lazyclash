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
        if scenario == "running":
            for value in ("12.0 KiB/s", "2.2 MiB/s", "4242", "PROXY", "香港 01", "日常订阅"):
                assert value in screen, "missing initial observation: " + value
        elif scenario == "drift":
            for value in ("v1.20.0", "v1.19.33", "Fixture running service differs from saved settings"):
                assert value in screen, "missing state discrepancy: " + value
        elif scenario == "stopped":
            assert words["dashboard.action.start"] in screen, "stopped service lost Start"
        else:
            assert "12.0 KiB/s" not in screen, "unavailable observation retained live metrics"

        # The first observation may already focus a card. Start explicitly from
        # navigation before checking both safe Enter transitions.
        terminal.key(b"\x1b")
        terminal.settle()
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

        expected = [lifecycle, words["dashboard.action.manage"], words["dashboard.action.profiles"]]
        if scenario != "unavailable":
            expected.extend([words["dashboard.action.restart"],
                             *[words["dashboard.mode." + mode] for mode in ("rule", "global", "direct")]])
        if scenario not in ("stopped", "unavailable"):
            expected.append(words["dashboard.action.openProxies"])

        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            terminal.key(b"\x1b[H")
            terminal.settle()
            preview.capture(terminal, output, "overview-%dx%d" % (width, height), args)
            terminal.key(b"\x1b")
            terminal.settle()
            assert_borderless(terminal)
            preview.capture(terminal, output, "navigation-%dx%d" % (width, height), args)
            # Navigation's next Tab stop is the safe card-browsing surface.
            terminal.key(b"\t")
            terminal.settle()
            assert_card_outline(terminal, profile, complete=scenario == "running" or height >= 32)
            preview.capture(terminal, output, "card-focus-%dx%d" % (width, height), args)
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
        assert calls["runtime_queries"] > 0, calls
        check_profile(preview, terminal, profile)
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS %s/%s/%s: five dimensions, borderless cards, complete yellow card focus, clean button focus, Tab reveal and read-only page actions; %s" %
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
    parser.add_argument("--scenario", choices=["running", "stopped", "unavailable", "drift"], action="append")
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
