#!/usr/bin/env python3
"""Verify Rules in a real controlling PTY using an opt-in memory backend.

Build with `go test -c ./internal/tui -o bin/rules-tui.test`. No core, network,
managed data or service is used. Optional PNGs redraw captured VT cells and
colors; they are not native terminal-app screenshots.
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


def wait_until(terminal, predicate, label, timeout=10):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        terminal.read(0.05)
        if not terminal.screen.synchronized and terminal.screen.state == "text" and predicate():
            return
        if terminal.closed:
            break
    raise AssertionError(label + "\n" + terminal.screen.text())


def position(terminal, label):
    for y, row in enumerate(terminal.screen.cells):
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(label):
                return x, y
    raise AssertionError("Visible text missing: " + label + "\n" + terminal.screen.text())


def click_cell(terminal, x, y):
    terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())
    terminal.settle()


def paste(terminal, value):
    terminal.key(b"\x1b[200~" + value.encode() + b"\x1b[201~")


def search(terminal, value):
    terminal.key(b"/")
    terminal.key(b"\x15")
    paste(terminal, value)
    terminal.key(b"\r")
    terminal.settle()


def toggle(terminal, label):
    terminal.click_text(label)
    terminal.settle()


def fixture_terminal(preview, binary, report, language, profile, count=60):
    environment = {"LAZYCLASH_RULES_PTY_REPORT": str(report), "LAZYCLASH_RULES_PTY_LANGUAGE": language}
    environment["LAZYCLASH_RULES_PTY_COUNT"] = str(count)
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestRulesPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"5")
        terminal.wait("private_ip")
        terminal.settle()
        preview.capture(terminal, output, "rules-wide", args)
        rows = terminal.screen.text().splitlines()
        first_y = position(terminal, "private_ip")[1]
        assert "RuleSet" in rows[first_y + 1] and "全球直连" in rows[first_y + 1], "rule metadata is not on its second row"

        # Both rows select their exact rule and keep the list open. Use
        # different rules so the metadata click must actually change focus.
        for payload, offset in (("qichiyu.com", 0), ("jumpdesktop.com", 1)):
            x, y = position(terminal, payload)
            click_cell(terminal, x, y + offset)
            screen = terminal.screen.text()
            assert words["rules.details"] not in screen and "private_ip" in screen, "rule click left the list"
            assert any(payload in row and re.search(r"[›▌]", row) for row in screen.splitlines()), "rule click did not select its exact row"
        preview.capture(terminal, output, "row-clicks-select", args)
        terminal.key(b"\r")
        terminal.wait(words["rules.details"])
        terminal.wait("jumpdesktop.com")
        terminal.wait("Jump Desktop")
        preview.capture(terminal, output, "selected-enter-details", args)
        terminal.key(b"\x1b")
        terminal.wait("private_ip")

        search(terminal, "GITHUB")
        terminal.wait("github_domain")
        assert "private_ip" not in terminal.screen.text(), "case-insensitive search kept an unrelated rule"
        terminal.key(b"\r")
        terminal.wait(words["rules.details"])
        terminal.wait("github_domain")
        terminal.key(b"\x1b")
        terminal.wait_absent(words["rules.details"])
        terminal.wait("github_domain")
        assert "GITHUB" in terminal.screen.text(), "return discarded the search query"
        preview.capture(terminal, output, "filtered-return", args)

        # Case and word options are independent and clear only removes text.
        search(terminal, "CaseSensitive")
        terminal.wait("CaseSensitive.example.invalid")
        terminal.wait("casesensitive.example.invalid")
        toggle(terminal, "Aa")
        terminal.wait_absent("casesensitive.example.invalid")
        terminal.wait("CaseSensitive.example.invalid")
        preview.capture(terminal, output, "case-sensitive", args)
        toggle(terminal, "Aa")
        search(terminal, "ai")
        terminal.key(b"\x1b[H")  # The current Domain rule can also match its type.
        terminal.settle()
        terminal.wait("private_domain")
        toggle(terminal, "ab")
        terminal.wait("ChatGPT")
        terminal.wait_absent("private_domain")
        preview.capture(terminal, output, "whole-word", args)
        toggle(terminal, "ab")

        search(terminal, "[")
        toggle(terminal, ".*")
        terminal.wait(words["rules.invalid_regex"])
        preview.capture(terminal, output, "invalid-regex", args)
        search(terminal, "(github|youtube)_domain")
        terminal.wait("github_domain")
        terminal.wait("youtube_domain")
        terminal.wait_absent(words["rules.invalid_regex"])
        assert "private_ip" not in terminal.screen.text(), "regex search kept an unrelated rule"
        preview.capture(terminal, output, "regex-results", args)
        toggle(terminal, ".*")

        search(terminal, "no-such-rule-pty")
        terminal.wait(words["rules.no_matches"])
        preview.capture(terminal, output, "no-matches", args)
        toggle(terminal, "×")
        terminal.key(b"\r")  # Clear returns focus to the search input.
        terminal.settle()
        terminal.wait("private_ip")

        # Scroll the list while its search options stay at their fixed cells.
        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.key(b"\x1b[<35;1;1M")  # Move away from filter help tooltips.
            terminal.settle()
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait("private_ip")
            toolbar = {label: position(terminal, label) for label in ("Aa", "ab", ".*")}
            for _ in range(5):
                terminal.key(("\x1b[<65;%d;%dM" % (width - 3, height // 2)).encode())
            terminal.settle()
            terminal.wait_absent("private_ip")
            assert toolbar == {label: position(terminal, label) for label in toolbar}, "search options moved with the list"
            preview.capture(terminal, output, "scrolled-%dx%d" % (width, height), args)
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait("private_ip")
            preview.capture(terminal, output, "rules-%dx%d" % (width, height), args)

        # A long disabled rule must retain complete fields in its details.
        search(terminal, "long-")
        terminal.key(b"\r")
        terminal.wait(words["rules.details"])
        terminal.wait(words["rules.disabled"])
        terminal.wait("4 / 9")
        preview.capture(terminal, output, "long-disabled-details", args)
        terminal.key(b"\x1b")
        search(terminal, "")
        terminal.key(b"\x1b[H")
        terminal.settle()
        terminal.key(b"g")
        terminal.wait("private_ip")
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["queries"] >= 2, calls
        assert calls["service_changes"] == calls["modes"] == calls["selections"] == 0, calls
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "application changed the terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: row clicks select, Enter details, search options/invalid regex, fixed toolbar and five resize dimensions; %s" %
            (language, profile, json.dumps(calls)), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
        if args.png:
            preview.png(terminal.screen, output / "failed.png", args.font, args.cjk_font, args.font_size)
        raise
    finally:
        terminal.cleanup()


def visible_rule_numbers(terminal):
    rows = terminal.screen.text().splitlines()
    result = []
    for index, row in enumerate(rows[:-1]):
        if not re.search(r"\b(?:RuleSet|DomainSuffix|Domain|Match)\b", rows[index + 1]):
            continue
        match = re.search(r"(?:^|\s)(\d+)\s{2,}\S", row)
        if match:
            result.append(int(match.group(1)))
    return result


def background(terminal, point):
    x, y = point
    color = terminal.screen.styles[y][x][1]
    if isinstance(color, str):
        return tuple(int(color[index:index + 2], 16) for index in (1, 3, 5))
    return tuple(color)


def measure_batch(terminal, payload, predicate, label, timeout=60):
    # One write deliberately batches terminal protocol events. This measures
    # local PTY -> app -> VT rendering, not a physical trackpad's event stream.
    start = time.monotonic()
    terminal.send(payload)
    wait_until(terminal, predicate, label, timeout)
    return round((time.monotonic() - start) * 1000, 3)


def exercise_scroll(preview, binary, output, count, args):
    output.mkdir(parents=True)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, "zh-Hans", "truecolor", count)
    metrics = {"rules": count, "size": "160x45", "motion_events": args.event_count + 1,
               "wheel_events_per_direction": args.event_count // 2,
               "measurement": "local PTY event batches through visible VT output; not physical trackpad input"}
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"5")
        terminal.wait("private_ip")
        terminal.settle()
        points = [position(terminal, value) for value in ("private_ip", "private_domain", "jumpdesktop.com")]
        hover = (60, 68, 76)
        assert all(background(terminal, point) != hover for point in points), "fixture began with an unexpected rule hover"
        payload = bytearray()
        # Consecutive positions in each row exercise redundant motion, while
        # crossing rows exercises distinct targets. Only the final event uses
        # row three, so observing that hover acknowledges the complete batch.
        for index in range(args.event_count):
            x, y = points[(index // 2) % 2]
            payload.extend(("\x1b[<35;%d;%dM" % (x + 1 + index % 2, y + 1)).encode())
        x, y = points[2]
        payload.extend(("\x1b[<35;%d;%dM" % (x + 1, y + 1)).encode())
        metrics["motion_batch_ms"] = measure_batch(terminal, bytes(payload),
            lambda: background(terminal, points[2]) == hover and all(background(terminal, point) != hover for point in points[:2]),
            "motion batch did not finish at its last rule")
        preview.capture(terminal, output, "motion-final-target", args)

        wheel_count = args.event_count // 2
        toolbar = {label: position(terminal, label) for label in ("Aa", "ab", ".*")}
        visible_count = len(visible_rule_numbers(terminal))
        expected_down = wheel_count + 1
        for button, expected, label in ((65, expected_down, "down"), (64, 1, "up")):
            payload = ("\x1b[<%d;150;25M" % button).encode() * wheel_count
            expected_range = list(range(expected, expected + visible_count))
            metrics["wheel_" + label + "_batch_ms"] = measure_batch(terminal, payload,
                lambda: visible_rule_numbers(terminal) == expected_range,
                "wheel %s batch did not reach rule %d" % (label, expected))
            assert toolbar == {key: position(terminal, key) for key in toolbar}, "wheel batch moved the fixed toolbar"
            metrics["wheel_" + label + "_visible_rules"] = visible_rule_numbers(terminal)
            pointer_hover = background(terminal, (149, 24)) == hover
            metrics["wheel_" + label + "_pointer_hover"] = pointer_hover
            if args.require_wheel_hover:
                assert pointer_hover, "wheel did not update hover at its current pointer position"
            preview.capture(terminal, output, "wheel-" + label + "-complete", args)

        # After the final acknowledgement, no queued motion or wheel event may
        # change the list. Poll for longer than a normal terminal refresh cycle.
        stable = terminal.screen.text(), tuple(tuple(row) for row in terminal.screen.styles)
        idle_start = time.monotonic()
        while time.monotonic() - idle_start < 1.2:
            terminal.read(0.03)
            if terminal.screen.synchronized:
                continue
            assert stable == (terminal.screen.text(), tuple(tuple(row) for row in terminal.screen.styles)), "view continued moving after the final input batch"
        metrics["idle_observation_ms"] = round((time.monotonic() - idle_start) * 1000, 3)
        metrics["idle_stable"] = True
        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["rules"] == count, calls
        assert calls["service_changes"] == calls["modes"] == calls["selections"] == 0, calls
        metrics["backend"] = calls
        (output / "timings.json").write_text(json.dumps(metrics, indent=2) + "\n")
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS scroll batch: " + json.dumps(metrics), flush=True)
    except Exception:
        (output / "timings-incomplete.json").write_text(json.dumps(metrics, indent=2) + "\n")
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
    parser.add_argument("--scenario", choices=["main", "scroll"], action="append", help="Default: main. Scroll runs fixed Chinese/TrueColor 160x45 event batches")
    parser.add_argument("--count", type=int, action="append", help="Rule count for scroll scenario; default: 670 and 2000")
    parser.add_argument("--event-count", type=int, default=24, help="Even motion-event count; each wheel batch has half this many events")
    parser.add_argument("--require-wheel-hover", action="store_true", help="Assert that wheel hover follows its current pointer cell (omit for pre-fix baselines)")
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("rules_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    if args.event_count < 2 or args.event_count % 2:
        parser.error("--event-count must be an even integer of at least 2")
    if any(count < max(60, args.event_count // 2 + 15) for count in args.count or []):
        parser.error("--count must leave enough rules for the requested wheel batch")
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    if "main" in (args.scenario or ("main",)):
        for language in args.lang or ("en", "zh-Hans"):
            for profile in args.profile or ("truecolor",):
                exercise(preview, args.binary, output / language / profile, language, profile, args)
    if "scroll" in (args.scenario or ()):
        for count in args.count or (670, 2000):
            exercise_scroll(preview, args.binary, output / ("scroll-%d" % count), count, args)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
