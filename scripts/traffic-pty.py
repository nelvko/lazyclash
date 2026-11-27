#!/usr/bin/env python3
"""Verify the real traffic chart through an isolated controlling PTY.

Build `go test -c ./internal/tui -o bin/traffic-tui.test` first. The opt-in
fixture observes traffic at wall-clock times. The waiting scenario keeps the
first frame pending. PNGs redraw captured VT cells.
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


SIZES = ((160, 45), (125, 32), (120, 32), (80, 24), (60, 20), (40, 16), (160, 45))
LIVE_KEYS = ("dashboard.overview.upload", "dashboard.overview.download", "dashboard.field.connections")
SUMMARY_KEYS = ("dashboard.overview.totalUpload", "dashboard.overview.totalDownload", "dashboard.field.coreMemory")


def labels(language):
    words = {}
    directory = Path(__file__).resolve().parents[1] / "internal" / "i18n" / "locales"
    for path in directory.glob(language + ".*.json"):
        words.update(json.loads(path.read_text()))
    return words


def wait_until(terminal, predicate, description, timeout=15):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        terminal.read(0.05)
        if not terminal.screen.synchronized and terminal.screen.state == "text" and predicate():
            return
        if terminal.closed:
            break
    raise AssertionError(description + "\n" + terminal.screen.text())


def progress(report):
    try:
        return json.loads(Path(str(report) + ".progress").read_text())
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


def fixture_terminal(preview, binary, report, language, profile, scenario):
    environment = {"LAZYCLASH_TRAFFIC_PTY_REPORT": str(report),
                   "LAZYCLASH_TRAFFIC_PTY_LANGUAGE": language,
                   "LAZYCLASH_TRAFFIC_PTY_SCENARIO": scenario}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestTrafficPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def return_navigation(terminal, words):
    terminal.settle()
    wide = sidebar_visible(terminal.screen.columns, terminal.screen.rows)
    prefix = words["navigation.sidebar" if wide else "navigation.menu"].split(" · ")[0]
    if terminal.screen.text().splitlines()[-1].lstrip().startswith(prefix):
        return
    terminal.key(b"\x1b")
    wait_until(terminal, lambda: terminal.screen.text().splitlines()[-1].lstrip().startswith(prefix),
               "Esc did not restore navigation focus", timeout=6)


def show_traffic(terminal, words):
    # End focuses the full-width traffic card at the bottom of the overview.
    return_navigation(terminal, words)
    terminal.key(b"\t")
    terminal.settle()
    terminal.key(b"\x1b[F")
    terminal.settle()
    title = words["dashboard.overview.traffic"]
    # A card taller than a short viewport is revealed at its lower edge. Scroll
    # within the body to inspect the plot, keeping the same semantic focus.
    for _ in range(15):
        if title in terminal.screen.text():
            break
        x, y = terminal.screen.columns - 3, max(3, terminal.screen.rows // 2)
        terminal.key(("\x1b[<64;%d;%dM" % (x, y)).encode())
        terminal.settle()
    terminal.wait(title)
    return title


def braille_cells(terminal):
    result = []
    for y, row in enumerate(terminal.screen.cells):
        for x, value in enumerate(row):
            if value and 0x2800 < ord(value[0]) <= 0x28ff:
                result.append((x, y, value, terminal.screen.styles[y][x][0]))
    return result


def rgb(value):
    if isinstance(value, str):
        return tuple(int(value[index:index + 2], 16) for index in (1, 3, 5))
    return tuple(value)


def locate(screen, label):
    for y, row in enumerate(screen.cells):
        for x, char in enumerate(row):
            if char == label[0] and "".join(row[x:]).startswith(label):
                return x, y
    raise AssertionError("missing label: " + label + "\n" + screen.text())


def reading_pattern(words, key, scenario):
    if scenario == "waiting":
        value = "--"
    elif key in LIVE_KEYS[:2]:
        value = "0 B/s" if scenario == "zero" else r"[\d.]+ (?:[KMGTPE]i)?B/s"
    elif key == "dashboard.field.connections":
        value = r"\d+"
    else:
        value = r"[\d.]+ (?:[KMGTPE]i)?B"
    return re.escape(words[key]) + " +" + value + r"(?:\s|$)"


def assert_plot(terminal, words, profile, scenario, colors=True):
    geometry = assert_fullwidth(terminal)
    cells = braille_cells(terminal)
    assert cells, "plot omitted its Braille reference grid"
    text = terminal.screen.text()
    rows = text.splitlines()
    plot_top = min(cell[1] for cell in cells)
    plot_bottom = max(cell[1] for cell in cells)
    screen = terminal.screen
    now_label = words["dashboard.overview.trafficNow"]
    _, axis_y = locate(screen, now_label)
    axis = "".join(screen.cells[axis_y][:geometry["right"] - 1])
    assert axis.endswith(now_label) and axis_y > plot_bottom, "the chart does not span the full card"
    before_axis = "\n".join(rows[:axis_y + 1])
    assert all(words[key] not in before_axis for key in LIVE_KEYS), "live readings appeared above or beside the chart"
    if screen.columns >= 120 and screen.rows >= 32:
        positions = [locate(screen, words[key]) for key in LIVE_KEYS]
        for i, (x, y) in enumerate(positions):
            label = words[LIVE_KEYS[i]]
            assert re.match(reading_pattern(words, LIVE_KEYS[i], scenario), "".join(screen.cells[y][x:])), "missing reading: " + label
            detail_x, detail_y = locate(screen, words[SUMMARY_KEYS[i]])
            assert axis_y < y < detail_y and detail_x == x, "grouped fields misaligned below plot: " + label
        if geometry["width"] >= 100:
            assert len({y for _, y in positions}) == 1 and positions == sorted(positions), "wide readings are not in three columns"
    for state, key in (("waiting", "dashboard.overview.trafficWaiting"), ("zero", "dashboard.overview.trafficIdle")):
        present = words[key] in "\n".join(rows[plot_top:plot_bottom + 1])
        assert present == (scenario == state), "unexpected plot status for " + scenario + ": " + words[key]
    assert "60" in text, "plot omitted its 60-second time window"
    if scenario in ("zero", "waiting"):
        # The three reference lines occupy their actual subdot rows: top/half
        # at subrow 0, bottom at subrow 3 for both four- and six-row plots.
        assert all(cell[2] in ("⠉", "⣀") for cell in cells), "zero samples drew a data curve"
        if profile != "no-color":
            assert all(max(rgb(cell[3])) < 128 for cell in cells), "zero samples drew bright data-colored dots"
    elif profile == "no-color" or not colors:
        assert any(cell[2] not in ("⠉", "⣀") for cell in cells), "reference grid was mistaken for data history"
    elif colors:
        hues = [rgb(cell[3]) for cell in cells]
        assert any(r > 190 and r >= g > b and g >= 110 for r, g, b in hues), "upload curve lost its orange dots"
        assert any(b > 190 and r < 180 and b > r + 40 and g > 120 for r, g, b in hues), "download curve lost its blue dots"
    return geometry


def assert_fullwidth(terminal):
    width, height = terminal.screen.columns, terminal.screen.rows
    wide = sidebar_visible(width, height)
    left, card_width = (22, width - 25) if wide else (2, width - 4)
    right = left + card_width - 1
    edges = {"┌": "┐", "│": "│", "└": "┘"}
    rows = [y for y, row in enumerate(terminal.screen.cells)
            if row[left] in edges and row[right] == edges[row[left]]]
    assert rows, "traffic card focus does not span the complete body width"
    return {"left": left, "right": right, "width": card_width, "visible_edge_rows": rows}


def check_profile(preview, terminal, profile):
    raw = bytes(terminal.raw)
    assert not preview.OSC_SET.search(raw), "chart changed the terminal palette"
    if profile == "256":
        assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
    if profile == "no-color":
        assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"


def exercise(preview, binary, output, language, profile, scenario, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile, scenario)
    try:
        terminal.wait("subscription.example")
        show_traffic(terminal, words)
        if scenario == "gap":
            wait_until(terminal, lambda: progress(report).get("phase") == "gap", "fixture did not omit its traffic sample")
            preview.capture(terminal, output, "traffic-missing-samples", args)
            wait_until(terminal, lambda: progress(report).get("phase") == "recovered", "traffic did not recover after its missing samples")
        elif scenario == "waiting":
            terminal.wait(words["dashboard.overview.trafficWaiting"])
        else:
            wait_until(terminal, lambda: progress(report).get("runtime_queries", 0) >= args.samples,
                       "fixture did not collect enough real-time traffic samples", timeout=max(20, args.samples * 2))
        wait_until(terminal, lambda: bool(braille_cells(terminal)), "Braille traffic history was not visible")
        geometry = {"initial": assert_plot(terminal, words, profile, scenario)}
        preview.capture(terminal, output, "traffic-wide-focus", args)
        return_navigation(terminal, words)
        assert not any(value in terminal.screen.text() for value in "┌┐└┘"), "traffic outline persisted after returning to navigation"
        preview.capture(terminal, output, "traffic-wide", args)
        if scenario == "restart":
            wait_until(terminal, lambda: progress(report).get("phase") == "restarted", "fixture did not observe its replacement process")
            show_traffic(terminal, words)
            wait_until(terminal, lambda: bool(braille_cells(terminal)) and "MiB/s" not in terminal.screen.text(),
                       "restarted process retained the previous traffic peak")
            preview.capture(terminal, output, "traffic-after-restart", args)
        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            show_traffic(terminal, words)
            wait_until(terminal, lambda: bool(braille_cells(terminal)), "resize hid all collected traffic history")
            geometry["%dx%d" % (width, height)] = assert_plot(terminal, words, profile, scenario, colors=width >= 60)
            preview.capture(terminal, output, "traffic-focus-%dx%d" % (width, height), args)
            return_navigation(terminal, words)
            assert not any(value in terminal.screen.text() for value in "┌┐└┘"), "card outline persisted after focus changed"
            preview.capture(terminal, output, "traffic-%dx%d" % (width, height), args)
            assert words["dashboard.footer.quit"] in terminal.screen.text().splitlines()[-1], "Quit hint left the footer"
            if (width, height) == (40, 16):
                needed = set(LIVE_KEYS + SUMMARY_KEYS)
                for _ in range(10):
                    visible = terminal.screen.text()
                    needed = {key for key in needed if not re.search(reading_pattern(words, key, scenario), visible)}
                    if not needed:
                        break
                    terminal.key(f"\x1b[<65;{width-3};{height//2}M".encode())
                    terminal.settle()
                assert not needed, f"40x16 could not reach these readings: {sorted(needed)}\n" + terminal.screen.text()
                preview.capture(terminal, output, "traffic-summary-40x16", args)
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["service_changes"] == calls["modes"] == calls["selections"] == calls["tun_changes"] == 0, calls
        if scenario == "waiting":
            assert calls["runtime_queries"] == 0, calls
        else:
            assert calls["runtime_queries"] >= args.samples, calls
        if scenario == "gap":
            assert calls["missing_samples"] == 3, calls
        if scenario == "restart":
            assert calls["pid"] == 4243, calls
        if scenario == "zero":
            assert calls["missing_samples"] == 0, calls
        check_profile(preview, terminal, profile)
        (output / "geometry.json").write_text(json.dumps(geometry, indent=2) + "\n")
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS %s/%s/%s: actual-time Braille history, grouped bottom readouts, resize and restore; %s" %
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
    parser.add_argument("--scenario", choices=["steady", "gap", "restart", "zero", "waiting"], action="append")
    parser.add_argument("--samples", type=int, default=5, help="Minimum actual one-second observations before the first capture")
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("traffic_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    if args.samples < 3:
        parser.error("--samples must be at least 3")
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "256", "no-color"):
            for scenario in args.scenario or ("steady",):
                exercise(preview, args.binary, output / language / profile / scenario, language, profile, scenario, args)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
