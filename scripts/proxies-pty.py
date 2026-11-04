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
    wait_until(terminal, lambda: any(re.search(r"✓\s*" + re.escape(label) + r"(?:\s|\]|$)", line)
               for line in terminal.screen.text().splitlines()), "routing mode was not confirmed: " + label)


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


def click_candidate(terminal, name, padding=0):
    terminal.settle()
    x, y = candidate_position(terminal, name)
    click_cell(terminal, x, y + padding)


def latency_span(terminal, name):
    x, y = candidate_position(terminal, name)
    row = "".join(terminal.screen.cells[y][x:])
    value = re.search(r"\d+ ms", row)
    assert value, "candidate has no numeric fixture latency: " + name
    return value.end()


def assert_three_columns(terminal):
    positions = [candidate_position(terminal, name) for name in MEMBERS]
    xs = sorted({x for x, _ in positions})
    assert len(xs) == 3, "wide grid did not keep three columns: " + repr(positions)
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


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True, exist_ok=True)
    report = output / "calls.json"
    words = labels(language)
    environment = {"LAZYCLASH_PROXIES_PTY_REPORT": str(report),
                   "LAZYCLASH_PROXIES_PTY_LANGUAGE": language}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestProxiesPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"2")
        terminal.wait("PROXY")
        terminal.settle()
        # The summary remains visible; a group click expands its candidates.
        terminal.click_text("PROXY")
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
        # Both blank padding rows belong to the card's selection surface.
        click_candidate(terminal, "HK 02", padding=-1)
        wait_until(terminal, lambda: chosen(terminal, "HK 02"), "top padding did not select its card")
        click_candidate(terminal, "HK 01", padding=2)
        wait_until(terminal, lambda: chosen(terminal, "HK 01"), "bottom padding did not select its card")
        terminal.settle()
        x, y = candidate_position(terminal, "HK 01")
        assert all(terminal.screen.cells[row][x - 4] == "▌" for row in range(y - 1, y + 3)), "selection rail did not span the padded card"
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
        wait_until(terminal, lambda: any(re.search(r"/\s*ChatGPT", line) for line in terminal.screen.text().splitlines()), "reference return lost search input")
        preview.capture(terminal, output, "search-reference-return", args)
        search(terminal, "")
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

        # Keep a node focused across terminal geometry changes. Each frame is
        # captured from the PTY after redraw rather than directly from Model.View.
        click_candidate(terminal, "HK 01")
        for width, height in [(160, 45), (200, 40), (240, 40), (120, 32), (80, 24), (60, 20), (40, 16), (120, 18)]:
            terminal.resize(width, height)
            terminal.settle()
            terminal.wait("HK 01")
            terminal.wait("ms")
            if width in (200, 240):
                assert_three_columns(terminal)
            preview.capture(terminal, output, "selected-%dx%d" % (width, height), args)

        # Short groups and member filters retain the same slots as a full grid.
        terminal.resize(240, 40)
        terminal.key(b"\x1b[H")
        terminal.key(b"\x1b[C")
        terminal.settle()
        slots = assert_three_columns(terminal)
        span = latency_span(terminal, "HK 01")
        search(terminal, "Hong Kong")
        terminal.click_text("▸ Hong Kong")
        terminal.wait("Trojan")
        terminal.settle()
        assert sorted(candidate_position(terminal, name)[0] for name in ("HK 01", "HK 02")) == slots[:2], "two-member group stretched its cards"
        assert latency_span(terminal, "HK 01") == span, "short-group card width changed"
        preview.capture(terminal, output, "two-members-240x40", args)
        search(terminal, "HK 01")
        terminal.click_text("▸ PROXY")
        terminal.wait("Shadowsocks")
        terminal.settle()
        assert candidate_position(terminal, "HK 01")[0] == slots[0], "single filtered member moved out of its first slot"
        assert latency_span(terminal, "HK 01") == span, "single filtered member stretched across the row"
        preview.capture(terminal, output, "one-member-filter-240x40", args)
        search(terminal, "")
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
        print("PASS %s/%s: selection/padding, references, search, sort, cancel/late results, strategies, resize/three-column slots" % (language, profile), flush=True)
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


if __name__ == "__main__":
    main()
