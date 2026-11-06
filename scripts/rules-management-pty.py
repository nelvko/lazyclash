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


def click_label(terminal, label, header=False):
    # Match a complete button face, never Save inside Saved or Update inside
    # Update all. Header-only lookup also avoids title/body label collisions.
    rows = terminal.screen.cells[:5] if header else terminal.screen.cells
    pattern = re.compile(r"(?<![^\s\[])" + re.escape(label) + r"(?=\s|\]|$)")
    for y, row in enumerate(rows):
        match = pattern.search("".join(row))
        if match:
            x = 0
            for index, cell in enumerate(row):
                if len("".join(row[:index])) == match.start():
                    x = index
                    break
            RULES.click_cell(terminal, x, y)
            return
    raise AssertionError("Button missing: " + label + "\n" + terminal.screen.text())


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
        click_label(terminal, words["rules.providers.title"], header=True)
        terminal.wait("remote_rules")
        terminal.wait("inline_rules")
        preview.capture(terminal, output, "providers-clean", args)
        terminal.key(b"\x1b[H")
        terminal.settle()
        # Home selects the first source in the backend's observed order.
        terminal.key(b"u")
        RULES.wait_until(terminal, lambda: report.exists() and calls(report)["provider_updates"] == ["remote_rules"], "single HTTP provider update did not finish")
        terminal.wait(words["rules.providers.success"])
        click_label(terminal, words["rules.providers.update_all"], header=True)
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
            preview.capture(terminal, output, "providers-filtered-%dx%d" % (width, height), args)
        terminal.key(b"\x1b")
        terminal.wait("source.example.invalid")
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
        print("PASS %s/%s: partial provider failure/retry, filtering and five sizes; %s" %
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
