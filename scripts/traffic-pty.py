#!/usr/bin/env python3
"""Verify the real traffic chart through an isolated controlling PTY.

Build `go test -c ./internal/tui -o bin/traffic-tui.test` first. The opt-in
fixture returns changing memory observations at real wall-clock times; chart
history is never prefilled or accelerated. PNGs redraw captured VT cells.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sys
import time
import uuid


SIZES = ((160, 45), (120, 32), (80, 24), (60, 20), (40, 16), (160, 45))


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
    wide = terminal.screen.columns >= 100 and terminal.screen.rows >= 20
    prefix = words["navigation.sidebar" if wide else "navigation.menu"].split(" · ")[0]
    if terminal.screen.text().splitlines()[-1].lstrip().startswith(prefix):
        return
    terminal.key(b"\x1b")
    wait_until(terminal, lambda: terminal.screen.text().splitlines()[-1].lstrip().startswith(prefix),
               "Esc did not restore navigation focus", timeout=6)


def show_traffic(terminal, words):
    # The six semantic cards are Core, Profile, Route, Access, Traffic, Environment.
    return_navigation(terminal, words)
    terminal.key(b"\t")
    terminal.settle()
    for _ in range(4):
        terminal.key(b"\x1b[B")
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


def assert_plot(terminal, words, profile, colors=True, zero=False):
    cells = braille_cells(terminal)
    assert cells, "plot omitted its Braille reference grid"
    text = terminal.screen.text()
    assert words["dashboard.overview.upload"] in text, "upload legend/readout missing"
    assert words["dashboard.overview.download"] in text, "download legend/readout missing"
    assert "60" in text, "plot omitted its 60-second time window"
    if zero:
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
    return len(cells)


def assert_fullwidth(terminal):
    width, height = terminal.screen.columns, terminal.screen.rows
    wide = width >= 100 and height >= 20
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
        terminal.wait("v1.19.33")
        show_traffic(terminal, words)
        if scenario == "gap":
            wait_until(terminal, lambda: progress(report).get("phase") == "gap", "fixture did not omit its traffic sample")
            preview.capture(terminal, output, "traffic-missing-samples", args)
            wait_until(terminal, lambda: progress(report).get("phase") == "recovered", "traffic did not recover after its missing samples")
        else:
            wait_until(terminal, lambda: progress(report).get("runtime_queries", 0) >= args.samples,
                       "fixture did not collect enough real-time traffic samples", timeout=max(20, args.samples * 2))
        wait_until(terminal, lambda: bool(braille_cells(terminal)), "Braille traffic history was not visible")
        assert_plot(terminal, words, profile, zero=scenario == "zero")
        geometry = {"initial": assert_fullwidth(terminal)}
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
            assert_plot(terminal, words, profile, colors=width >= 60, zero=scenario == "zero")
            geometry["%dx%d" % (width, height)] = assert_fullwidth(terminal)
            preview.capture(terminal, output, "traffic-focus-%dx%d" % (width, height), args)
            return_navigation(terminal, words)
            assert not any(value in terminal.screen.text() for value in "┌┐└┘"), "card outline persisted after focus changed"
            preview.capture(terminal, output, "traffic-%dx%d" % (width, height), args)
            assert words["dashboard.footer.quit"] in terminal.screen.text().splitlines()[-1], "Quit hint left the footer"
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["service_changes"] == calls["modes"] == calls["selections"] == calls["tun_changes"] == 0, calls
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
        print("PASS %s/%s/%s: actual-time Braille history, fullwidth card, five sizes and restore; %s" %
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
    parser.add_argument("--scenario", choices=["steady", "gap", "restart", "zero"], action="append")
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
