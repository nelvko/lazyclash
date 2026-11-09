#!/usr/bin/env python3
"""Verify proxy providers and page headers through an isolated real PTY.

Build: go test -c ./internal/tui -o bin/proxy-providers-tui.test
The opt-in fixture uses memory state, with no network, storage or service changes.
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


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


PANEL = load_module("proxy_provider_panel_helpers", Path(__file__).with_name("rules-management-pty.py"))
PROXIES = load_module("proxy_provider_group_helpers", Path(__file__).with_name("proxies-pty.py"))
RULES = PANEL.RULES


def fixture_terminal(preview, binary, report, language, profile):
    environment = {"LAZYCLASH_PROXY_PROVIDERS_PTY_REPORT": str(report),
                   "LAZYCLASH_PROXY_PROVIDERS_PTY_LANGUAGE": language}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestProxyProvidersPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def calls(report):
    return json.loads(report.read_text())


def assert_header(terminal, words, width):
    rows = terminal.screen.text().splitlines()
    divider = next(i for i, row in enumerate(rows) if "▔" in row)
    header = "\n".join(rows[:divider])
    assert words["header.refresh"] not in header and "↻" not in header, "manual refresh returned"
    assert not re.search(r"\b\d+\s+groups\b|\d+\s*个代理组", header), "group total returned"
    assert words["providers.search.proxies"] not in terminal.screen.text(), "main-page search returned"
    mode_positions = [RULES.position(terminal, words["dashboard.mode." + mode]) for mode in ("rule", "global", "direct")]
    if width >= 50:
        assert all(y < divider for _, y in mode_positions), "modes did not move into page header"
        assert [x for x, _ in mode_positions] == sorted(x for x, _ in mode_positions), "mode order changed"
        provider_x, provider_y = PANEL.label_position(terminal, words["providers.title.proxies"], header=True)
        assert provider_y < divider and provider_x > mode_positions[-1][0], "provider action not right-aligned after modes"
    else:
        assert all(y > divider for _, y in mode_positions), "narrow modes disappeared from body"


def search(terminal, value):
    RULES.search(terminal, value)


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = RULES.labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("v1.19.33")
        terminal.key(b"2")
        terminal.wait("PROXY")
        terminal.settle()
        for width, height in ((160, 45), (80, 24), (60, 20), (40, 16), (160, 45)):
            terminal.resize(width, height)
            terminal.settle()
            assert_header(terminal, words, width)
            preview.capture(terminal, output, "header-%dx%d" % (width, height), args)
        PANEL.click_label(terminal, words["providers.title.proxies"], header=True)
        terminal.wait("remote_nodes")
        terminal.wait(words["providers.close"])
        preview.capture(terminal, output, "providers-open", args)
        # Search owns ordinary keys; Escape ends editing without closing.
        terminal.key(b"/")
        terminal.key(b"\x15")
        RULES.paste(terminal, "q123")
        terminal.wait(words["providers.no_matches"])
        terminal.key(b"\x1b")
        terminal.wait(words["providers.close"])
        search(terminal, "")
        terminal.wait("remote_nodes")
        terminal.key(b"\x1b[H")
        terminal.key(b"u")
        RULES.wait_until(terminal, lambda: calls(report)["updates"] == ["remote_nodes"], "single provider did not update")
        terminal.wait(words["providers.success"])
        PANEL.click_label(terminal, words["providers.update_all"])
        RULES.wait_until(terminal, lambda: len(calls(report)["attempts"] or []) == 6 and len(calls(report)["updates"] or []) == 5, "batch did not finish external sources")
        failure = words["providers.failed"] % "fixture local source read failed"
        terminal.wait(failure)
        result = calls(report)
        assert result["failures"] == ["local_nodes"] and result["max_active"] == 3, result
        assert all(not name.startswith(("inline", "compatible")) for name in result["attempts"]), result
        preview.capture(terminal, output, "providers-partial-failure", args)
        PANEL.click_label(terminal, words["providers.retry"])
        RULES.wait_until(terminal, lambda: len(calls(report)["updates"] or []) == 6, "failed file provider did not retry")
        terminal.wait_absent(failure)
        search(terminal, "compatible")
        terminal.wait("compatible_nodes")
        terminal.wait(words["providers.readonly"])
        attempts = len(calls(report)["attempts"])
        terminal.key(b"\x1b[H")
        terminal.key(b"u")
        terminal.settle()
        assert len(calls(report)["attempts"]) == attempts, "read-only provider updated"
        search(terminal, "inline")
        terminal.key(b"\x1b[H")
        terminal.settle()
        terminal.wait("inline_nodes")
        for width, height in PANEL.SIZES:
            terminal.resize(width, height)
            terminal.settle()
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait("inline_nodes")
            controls = {key: PANEL.label_position(terminal, words[key]) for key in ("providers.update_all", "providers.close")}
            x, y = RULES.position(terminal, "inline_nodes")
            PANEL.wheel(terminal, x + 1, y + 1, count=8)
            terminal.wait_absent("inline_nodes")
            def controls_restored():
                try:
                    return controls == {key: PANEL.label_position(terminal, words[key]) for key in controls}
                except AssertionError:
                    return False  # A resize/wheel redraw can split a fixed footer row.
            RULES.wait_until(terminal, controls_restored, "panel controls scrolled")
            preview.capture(terminal, output, "providers-scroll-%dx%d" % (width, height), args)
        PANEL.click_label(terminal, words["providers.close"])
        terminal.wait_absent("inline-extra")
        PROXIES.open_group(terminal, "PROXY")
        terminal.wait("Updated node")
        preview.capture(terminal, output, "updated-main-list", args)
        PANEL.click_label(terminal, words["providers.title.proxies"], header=True)
        terminal.wait("inline")
        search(terminal, "remote_slow")
        terminal.wait("remote_slow")
        terminal.key(b"\x1b[H")
        terminal.key(b"u")
        RULES.wait_until(terminal, lambda: len(calls(report)["attempts"]) == attempts + 1, "slow update never started")
        terminal.key(b"q")
        terminal.wait_absent("remote_slow")
        RULES.wait_until(terminal, lambda: calls(report)["canceled"] == 1, "closing panel did not cancel update")
        PANEL.click_label(terminal, words["providers.title.proxies"], header=True)
        terminal.wait("remote_slow")
        terminal.key(b"q")
        terminal.wait_absent("remote_slow")
        terminal.exit()
        result = calls(report)
        assert result["service"] == result["modes"] == 0 and result["reads"] >= 3, result
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "provider panel changed terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: mode headers, provider search, update/retry/batch, read-only sources, resize/scroll, close cancellation and refreshed main list; %s" %
              (language, profile, json.dumps(result)), flush=True)
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
    preview = load_module("proxy_provider_preview", Path(__file__).with_name("onboarding-pty.py"))
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
