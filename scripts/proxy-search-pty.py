#!/usr/bin/env python3
"""Exercise proxy search, current-node entry and compact layouts in a real PTY.

Uses the existing in-memory proxy fixture; no core, service or network changes.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--lang", action="append", choices=("en", "zh-Hans"))
    sys.dont_write_bytecode = True
    proxy = load("proxy_search_fixture", "proxies-pty.py")
    preview = load("proxy_search_preview", "onboarding-pty.py")
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    for language in args.lang or ("en", "zh-Hans"):
        output = args.output.resolve() / language
        output.mkdir(parents=True, exist_ok=True)
        report = output / "calls.json"
        words = proxy.labels(language)
        terminal = proxy.fixture_terminal(preview, args.binary, report, language, "truecolor")
        try:
            terminal.wait("v1.19.33")
            terminal.key(b"2")
            terminal.wait("PROXY")
            terminal.settle()
            proxy.open_group(terminal, "AUTO")
            terminal.settle()
            assert re.search(r"›\s*HK 02", terminal.screen.text()), "group entry did not focus its current node"
            for width, height in ((160, 40), (80, 24), (60, 18), (120, 18), (160, 40)):
                terminal.resize(width, height)
                terminal.settle()
                assert any(words["proxies.search_group"] % name in terminal.screen.text() for name in ("AUTO", "PROXY")), "fixed group label disappeared"
                terminal.key(b"/")
                terminal.wait(words["proxies.search_placeholder"])
                terminal.key(b"\x1b[200~hk 02\x1b[201~")
                terminal.wait("hk 02")
                terminal.settle()
                assert "hk 02" in terminal.screen.text(), "search input lost pasted text"
                assert "HK 02" in proxy.candidate_order(terminal, words["proxies.enter"]), "search lost its matching node"
                preview.capture(terminal, output, "search-%dx%d" % (width, height), args)
                terminal.key(b"\x15q1sti")
                terminal.wait(words["proxies.search_no_match"])
                assert not terminal.closed, "search text triggered a global shortcut"
                preview.capture(terminal, output, "no-match-%dx%d" % (width, height), args)
                terminal.key(b"\x15\x1b")
                terminal.wait_absent(words["proxies.search_placeholder"])
                terminal.settle()
                assert words["proxies.search_placeholder"] not in terminal.screen.text(), "empty search did not close"
                preview.capture(terminal, output, "members-%dx%d" % (width, height), args)
            terminal.exit()
            calls = json.loads(report.read_text())
            assert calls["selections"] == calls["modes"] == calls["tests"] == 0, calls
            print("PASS %s: current-node focus, search, no-match, shortcut isolation, compact resize; no backend mutations" % language, flush=True)
        except BaseException:
            preview.capture(terminal, output, "failed", args)
            raise
        finally:
            terminal.cleanup()


if __name__ == "__main__":
    main()
