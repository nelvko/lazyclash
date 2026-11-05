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
        if not terminal.screen.synchronized and predicate():
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


def fixture_terminal(preview, binary, report, language, profile):
    environment = {"LAZYCLASH_RULES_PTY_REPORT": str(report), "LAZYCLASH_RULES_PTY_LANGUAGE": language}
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

        # Clicking either row must open the same rule rather than its neighbor.
        x, y = position(terminal, "jumpdesktop.com")
        click_cell(terminal, x, y + 1)
        terminal.wait(words["rules.details"])
        terminal.wait("jumpdesktop.com")
        terminal.wait("Jump Desktop")
        preview.capture(terminal, output, "metadata-click-details", args)
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
        print("PASS %s/%s: two-row clicks, search options/invalid regex, details, fixed toolbar and five resize dimensions; %s" %
            (language, profile, json.dumps(calls)), flush=True)
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
    spec = importlib.util.spec_from_file_location("rules_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor",):
            exercise(preview, args.binary, output / language / profile, language, profile, args)
    print("Evidence:", output)


if __name__ == "__main__":
    main()
