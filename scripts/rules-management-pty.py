#!/usr/bin/env python3
"""Exercise rule providers in a real controlling PTY.

Build with `go test -c ./internal/tui -o bin/rules-management-tui.test`.
The opt-in fixture has isolated memory state and never contacts a core,
managed storage, network or service. Optional PNGs redraw captured VT cells.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys
import time
import uuid


sys.dont_write_bytecode = True
SIZES = ((120, 32), (80, 24), (60, 20), (40, 16), (160, 45))


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


RULES = load_module("rule_management_helpers", Path(__file__).with_name("rules-pty.py"))


def fixture_terminal(preview, binary, report, language, profile):
    environment = {
        "LAZYCLASH_RULE_MANAGEMENT_PTY_REPORT": str(report),
        "LAZYCLASH_RULE_MANAGEMENT_PTY_LANGUAGE": language,
    }
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestRuleManagementPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def label_position(terminal, label, header=False):
    # Match a complete button face, never Update inside Update all. Modal
    # controls may sit in the middle of the screen rather than the page header.
    rows = terminal.screen.cells[:5] if header else terminal.screen.cells
    pattern = re.compile(r"(?<![^\s\[])" + re.escape(label) + r"(?=\s|\]|$)")
    positions = []
    for y, row in enumerate(rows):
        for match in pattern.finditer("".join(row)):
            x = 0
            for index, cell in enumerate(row):
                if len("".join(row[:index])) == match.start():
                    x = index
                    break
            positions.append((x, y))
    if positions:
        # The footer's help also says Close. Its actual button is aligned to
        # the right, so choose the rightmost complete label.
        return max(positions)
    raise AssertionError("Button missing: " + label + "\n" + terminal.screen.text())


def click_label(terminal, label, header=False):
    RULES.click_cell(terminal, *label_position(terminal, label, header))


def wheel(terminal, x, y, down=True, count=1):
    code = 65 if down else 64
    terminal.key(("\x1b[<%d;%d;%dM" % (code, x + 1, y + 1)).encode() * count)
    terminal.settle()


def rule_positions(terminal):
    result = {}
    pattern = re.compile(r"source(?:-\d+)?\.example\.invalid")
    for y, row in enumerate(terminal.screen.cells):
        for match in pattern.finditer("".join(row)):
            result[match.group()] = y
    return result


def fixed_controls(terminal, words):
    return {key: label_position(terminal, words[key]) for key in
            ("rules.providers.update_all", "rules.providers.close")}


def calls(report):
    return json.loads(report.read_text())


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = RULES.labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"5")
        terminal.wait("source.example.invalid")
        RULES.search(terminal, "source")
        terminal.key(b"\x1b[F")
        terminal.settle()
        terminal.wait("source-59.example.invalid")
        main_positions = rule_positions(terminal)
        assert "source.example.invalid" not in main_positions, "main-list fixture never scrolled"
        click_label(terminal, words["rules.providers.title"], header=True)
        terminal.wait("remote_rules")
        terminal.wait("inline_rules")
        terminal.wait(words["rules.providers.close"])
        preview.capture(terminal, output, "providers-clean", args)

        # A modal owns normal keys, clicks and wheels. Neither navigation nor
        # service shortcuts may reach the dimmed main page behind it.
        controls = fixed_controls(terminal, words)
        terminal.key(b"123r?")
        terminal.settle()
        nav_x, nav_y = RULES.position(terminal, words["dashboard.page.profiles"], max_column=20)
        RULES.click_cell(terminal, nav_x, nav_y)
        wheel(terminal, 24, 20, count=6)
        terminal.wait("remote_rules")
        assert fixed_controls(terminal, words) == controls, "background input displaced modal controls"
        terminal.key(b"/")
        terminal.key(b"q123")
        terminal.settle()
        terminal.wait(words["rules.providers.no_matches"])
        terminal.key(b"\x1b")  # Finish input, retaining the modal.
        terminal.settle()
        terminal.wait(words["rules.providers.close"])
        terminal.wait(words["rules.providers.no_matches"])
        RULES.search(terminal, "")
        terminal.wait("remote_rules")

        # A long Inline-only fixture verifies independent body scrolling. The
        # title/actions and footer stay fixed while body rows leave the screen.
        first_x, first_y = RULES.position(terminal, "remote_rules")
        wheel(terminal, first_x + 2, first_y + 1, count=8)
        terminal.wait_absent("remote_rules")
        assert fixed_controls(terminal, words) == controls, "modal title or footer scrolled with providers"
        preview.capture(terminal, output, "providers-scrolled", args)
        terminal.key(b"\x1b[H")
        terminal.settle()
        # Home selects the first source in the backend's observed order.
        terminal.key(b"u")
        RULES.wait_until(terminal, lambda: report.exists() and calls(report)["provider_updates"] == ["remote_rules"], "single HTTP provider update did not finish")
        terminal.wait(words["rules.providers.success"])
        click_label(terminal, words["rules.providers.update_all"])
        RULES.wait_until(terminal, lambda: len(calls(report)["provider_attempts"] or []) == 3, "Update all did not attempt its external providers")
        failure = words["rules.providers.failed"] % "fixture local source read failed"
        terminal.wait(failure)
        terminal.settle()
        partial = calls(report)
        assert sorted(partial["provider_attempts"]) == ["local_rules", "remote_rules", "remote_rules"], partial
        assert partial["provider_updates"] == ["remote_rules", "remote_rules"] and partial["provider_failures"] == ["local_rules"], partial
        preview.capture(terminal, output, "providers-partial-failure", args)
        click_label(terminal, words["rules.providers.retry"])
        RULES.wait_until(terminal, lambda: len(calls(report)["provider_attempts"] or []) == 4, "failed File source did not retry independently")
        terminal.wait_absent(failure)
        terminal.settle()
        assert sorted(calls(report)["provider_updates"]) == ["local_rules", "remote_rules", "remote_rules"], calls(report)
        preview.capture(terminal, output, "providers-updated", args)
        terminal.key(b"/")
        RULES.paste(terminal, "inline")
        terminal.key(b"\r")
        terminal.settle()
        terminal.wait("inline_rules")
        terminal.wait_absent("remote_rules")
        assert words["rules.providers.inline_readonly"] in terminal.screen.text(), "inline source was not presented read only"
        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            terminal.wait("inline_rules")
            fixed = fixed_controls(terminal, words)
            first_x, first_y = RULES.position(terminal, "inline_rules")
            wheel(terminal, first_x + 1, first_y + 1, count=8)
            terminal.wait_absent("inline_rules")
            assert fixed_controls(terminal, words) == fixed, "resizing mixed fixed controls with scrolling content"
            preview.capture(terminal, output, "providers-scrolled-%dx%d" % (width, height), args)
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait("inline_rules")
            preview.capture(terminal, output, "providers-filtered-%dx%d" % (width, height), args)
        click_label(terminal, words["rules.providers.close"])
        terminal.wait("source-59.example.invalid")
        assert rule_positions(terminal) == main_positions, "closing modal lost the main rule-list position"
        assert any(re.search(r"(?:^|\s)source\s+" + re.escape(words["search.clear"]) + r"(?:\s|$)", row) for row in terminal.screen.text().splitlines()), "closing modal lost the main rule search"
        preview.capture(terminal, output, "main-rules-restored", args)
        terminal.key(b"\r")  # Focus returns to the original header button.
        terminal.wait("inline_rules")
        terminal.settle()
        terminal.key(b"q")
        terminal.wait_absent("inline_rules")
        terminal.settle()
        terminal.wait("source-59.example.invalid")
        assert rule_positions(terminal) == main_positions, "q-close lost the main rule-list position"
        terminal.exit()
        result = calls(report)
        assert result["service_changes"] == result["modes"] == result["selections"] == 0, result
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "application changed the terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        summary = result
        print("PASS %s/%s: modal input ownership, independent scrolling, partial failure/retry, main-list restoration and five sizes; %s" %
            (language, profile, json.dumps(summary)), flush=True)
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
    preview = load_module("rule_management_preview", Path(__file__).with_name("onboarding-pty.py"))
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
