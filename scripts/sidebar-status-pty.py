#!/usr/bin/env python3
"""Verify global core status through a real controlling PTY.

Uses the existing Overview and Logs in-memory fixtures in an explicit Go test
binary. No managed data, core, network or system service is touched. Optional
PNGs redraw actual PTY cells and colors, not a native terminal screenshot.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sys
import time
import uuid

from tui_layout import sidebar_visible


SIZES = ((160, 45), (120, 32), (120, 26), (120, 25), (120, 18),
         (120, 10), (120, 9), (114, 24), (80, 24), (40, 16), (160, 45))
PAGES = ("overview", "proxies", "profiles", "connections", "rules", "logs", "settings")


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def status_positions(terminal, label):
    return [(x, y) for y, row in enumerate(terminal.screen.cells)
            for x, cell in enumerate(row) if cell and "".join(row[x:]).startswith(label)]


def assert_status(terminal, words, overview_page=False):
    width, height = terminal.screen.columns, terminal.screen.rows
    header_y = 1 if height >= 24 else 0
    label = words["dashboard.status.running"]
    sidebar_label = "● " + words["dashboard.sidebar.coreStatus"] % label
    points = status_positions(terminal, sidebar_label)
    if sidebar_visible(width, height):
        assert len(points) == 1, "missing or repeated sidebar core status: " + repr(points)
        x, y = points[0]
        assert x < 20 and y >= height - 7, "core status is outside the fixed sidebar footer"
        header = "".join(terminal.screen.cells[header_y][20:])
        assert "● " + label not in header, "page title retained global core status"
        rates = []
        for rate_y, row in enumerate(terminal.screen.cells):
            fields = "".join(row[:20]).split()
            if len(fields) >= 2 and fields[0] in ("↑", "↓"):
                rates.append((fields[0], rate_y))
        if overview_page or height <= 10:
            assert not rates, "sidebar retained rates on Overview or at minimal height"
        elif rates or height >= 32:
            assert rates == [("↑", y - 2), ("↓", y - 1)], "sidebar rates are missing or out of place"
    else:
        assert not points, "navigation drawer repeated global core status"
        header = "".join(terminal.screen.cells[header_y])
        assert "● " + label in header, "compact top bar lost core status"
    assert len(terminal.screen.cells) == height
    assert all(len(row) == width for row in terminal.screen.cells)
    return points[0][1] if points else None


def assert_title(terminal, words, page):
    header_y = 1 if terminal.screen.rows >= 24 else 0
    assert words["dashboard.page." + page] in "".join(terminal.screen.cells[header_y]), \
        "page navigation did not preserve its header"


def exercise_overview(preview, overview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = overview.labels(language)
    report = output / "calls.json"
    terminal = overview.fixture_terminal(preview, binary, report, language, profile, "running")
    try:
        terminal.wait("v1.19.33")
        baseline = None
        for number, page in enumerate(PAGES, 1):
            overview.return_navigation(terminal, words)
            terminal.key(str(number).encode())
            terminal.settle()
            assert_title(terminal, words, page)
            status_y = assert_status(terminal, words, overview_page=page == "overview")
            if baseline is None:
                baseline = status_y
            assert status_y == baseline, "changing pages moved the sidebar core status"
            preview.capture(terminal, output, "page-" + page, args)

        overview.return_navigation(terminal, words)
        terminal.key(b"6")
        for index, (width, height) in enumerate(SIZES):
            terminal.resize(width, height)
            terminal.settle()
            assert_title(terminal, words, "logs")
            assert_status(terminal, words)
            preview.capture(terminal, output, "%02d-logs-%dx%d" % (index, width, height), args)
            if (width, height) == (120, 9):
                overview.return_navigation(terminal, words)
                terminal.key(b"\x1b[F")
                terminal.settle()
                assert_title(terminal, words, "settings")
                assert any(x < 20 for x, _ in status_positions(terminal, words["dashboard.page.settings"])), \
                    "short sidebar could not reveal its last navigation destination"
                assert_status(terminal, words)
                preview.capture(terminal, output, "short-sidebar-last-page", args)
                terminal.key(b"\x1b[H")
                terminal.settle()
                assert_title(terminal, words, "overview")
                terminal.key(b"6")
            if (width, height) == (80, 24):
                terminal.click_text(words["dashboard.menu"])
                terminal.settle()
                assert_status(terminal, words)
                preview.capture(terminal, output, "compact-drawer", args)
                terminal.click_text(words["dashboard.page.settings"])
                terminal.settle()
                assert_title(terminal, words, "settings")
                assert_status(terminal, words)
                overview.return_navigation(terminal, words)
                terminal.key(b"6")

        terminal.key(b"\x14")  # Ctrl+T: status must survive hidden key hints.
        terminal.settle()
        assert_status(terminal, words)
        assert words["dashboard.footer.quit"] not in terminal.screen.text().splitlines()[-1], \
            "Ctrl+T did not hide key hints"
        preview.capture(terminal, output, "hints-hidden", args)
        terminal.resize(80, 24)
        terminal.settle()
        assert_status(terminal, words)
        preview.capture(terminal, output, "compact-hints-hidden", args)
        terminal.key(b"\x14")
        terminal.exit()
        calls = json.loads(report.read_text())
        assert all(calls[key] == 0 for key in ("service_changes", "modes", "selections", "tun_changes", "settings_writes")), calls
        overview.check_profile(preview, terminal, profile)
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS %s/%s: seven pages, %d resizes, short navigation, compact drawer, hidden hints; %s" %
              (language, profile, len(SIZES), json.dumps(calls)), flush=True)
    except Exception:
        preview.capture(terminal, output, "failed", args)
        raise
    finally:
        terminal.cleanup()


def exercise_logs(preview, overview, logs, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = logs.labels(language)
    report = output / "calls.json"
    terminal = logs.fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"6")
        terminal.wait(words["logs.capturing"])
        for width, height in ((160, 45), (80, 24)):
            terminal.resize(width, height)
            terminal.settle()
            assert_status(terminal, words)
            terminal.wait(words["logs.capturing"])
            preview.capture(terminal, output, "live-%dx%d" % (width, height), args)
            terminal.click_text(words["logs.pause_display"])
            terminal.wait(words["logs.resume_display"])
            terminal.wait(words["logs.display_paused"])
            if sidebar_visible(width, height):
                terminal.wait(words["header.paused"])
            assert_status(terminal, words)
            preview.capture(terminal, output, "paused-%dx%d" % (width, height), args)
            terminal.click_text(words["logs.resume_display"])
            terminal.wait(words["logs.capturing"])
        terminal.exit()
        calls = json.loads(report.read_text())
        assert all(calls[key] == 0 for key in ("service_changes", "modes", "selections")), calls
        overview.check_profile(preview, terminal, profile)
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS %s/%s: Logs Live/Paused are independent of global core status; %s" %
              (language, profile, json.dumps(calls)), flush=True)
    except Exception:
        preview.capture(terminal, output, "failed", args)
        raise
    finally:
        terminal.cleanup()


def main():
    sys.dont_write_bytecode = True
    preview = module("sidebar_status_preview", "onboarding-pty.py")
    overview = module("sidebar_status_overview", "overview-pty.py")
    logs = module("sidebar_status_logs", "logs-pty.py")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--lang", choices=["en", "zh-Hans"], action="append")
    parser.add_argument("--profile", choices=["truecolor", "no-color"], action="append")
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "no-color"):
            exercise_overview(preview, overview, args.binary, output / language / profile / "navigation", language, profile, args)
            exercise_logs(preview, overview, logs, args.binary, output / language / profile / "logs", language, profile, args)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
