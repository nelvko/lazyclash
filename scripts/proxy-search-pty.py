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


def visible_groups(terminal, proxy):
    groups = []
    for name in ("PROXY", "AUTO", "ChatGPT", "YouTube", "Hong Kong", "Japan", "BALANCE", "EMPTY", "GLOBAL"):
        try:
            _, y, _, _ = proxy.group_header(terminal, name)
            groups.append((y, name))
        except AssertionError:
            continue
    return [name for _, name in sorted(groups)]


def view_context(terminal, proxy, words):
    _, _, _, header_bottom = proxy.proxy_header_positions(terminal, words)
    left = 22 if proxy.sidebar_visible(terminal.screen.columns, terminal.screen.rows) else 2
    # Retain row positions, group disclosure state and the focus marker. Ignore
    # header hover and context-sensitive footer text when comparing restoration.
    return ["".join(row[left:]).rstrip()
            for row in terminal.screen.cells[header_bottom + 1:-1]]


def open_search(terminal, proxy, words, method):
    if method == "slash":
        terminal.key(b"/")
    else:
        _, _, provider, _ = proxy.proxy_header_positions(terminal, words)
        row = terminal.screen.cells[provider[1]]
        positions = [x for x in range(provider[0])
                     if "".join(row[x:]).startswith(words["proxies.search"]) or
                     row[x] == "/" and (x == 0 or row[x - 1].isspace()) and
                     (x + 1 == len(row) or row[x + 1].isspace())]
        assert positions, "header search action is missing\n" + terminal.screen.text()
        proxy.click_cell(terminal, positions[-1], provider[1])
    terminal.wait(words["proxies.search_placeholder"])
    terminal.settle()


def paste_query(terminal, value):
    terminal.key(b"\x1b[200~" + value.encode() + b"\x1b[201~")
    terminal.wait(value)
    terminal.settle()


def escape_restores(terminal, proxy, words, before, query):
    terminal.key(b"\x1b")
    terminal.settle()
    assert words["proxies.search_placeholder"] not in terminal.screen.text(), "Esc left the search input visible"
    after = view_context(terminal, proxy, words)
    assert after == before, "Esc did not restore the original scroll, focus and expansions after %r\n%s" % (query, terminal.screen.text())


def expanded_restoration(terminal, proxy, preview, output, words, args):
    terminal.resize(160, 40)
    terminal.settle()
    proxy.open_group(terminal, "PROXY")
    proxy.open_group(terminal, "AUTO")
    terminal.key(b"\x1b[F")
    terminal.settle()
    before = view_context(terminal, proxy, words)
    assert "PROXY" not in visible_groups(terminal, proxy), "restoration fixture did not scroll away from the first group"
    assert any(re.search(r"›\s*EMPTY\b", row) for row in before), "restoration fixture lost its original group focus"
    preview.capture(terminal, output, "before-search-scrolled", args)

    open_search(terminal, proxy, words, "slash")
    placeholder = words["proxies.search_placeholder"]
    search_y = next(y for y, row in enumerate(terminal.screen.cells) if placeholder in "".join(row))
    proxy.wheel_body(terminal)
    assert words["proxies.search_group"] % "PROXY" in terminal.screen.text(), "scrolling matching nodes did not expose the sticky group name"
    assert placeholder in "".join(terminal.screen.cells[search_y]), "sticky group name moved the pinned search field"
    preview.capture(terminal, output, "search-pinned-with-sticky-group", args)
    paste_query(terminal, "YouTube")
    assert visible_groups(terminal, proxy) == ["YouTube"], "group-name search retained unrelated groups"
    terminal.key(b"\r")
    terminal.settle()
    assert "YouTube" in terminal.screen.text(), "Enter discarded group search results"
    assert re.search(r"›\s*YouTube\b", terminal.screen.text()), "Enter did not focus the matching group"
    preview.capture(terminal, output, "group-name-results", args)
    escape_restores(terminal, proxy, words, before, "YouTube")
    preview.capture(terminal, output, "group-results-escape-restored", args)

    open_search(terminal, proxy, words, "click")
    paste_query(terminal, "hk 02")
    assert set(proxy.candidate_order(terminal, words["proxies.enter"])) == {"HK 02"}, "node-name search retained unrelated nodes"
    preview.capture(terminal, output, "node-name-editing", args)
    escape_restores(terminal, proxy, words, before, "hk 02")
    preview.capture(terminal, output, "node-editing-escape-restored", args)
    terminal.key(b"\x1b[H")
    terminal.settle()
    for name in ("PROXY", "AUTO"):
        assert proxy.group_header(terminal, name)[3].rstrip().endswith("▾"), "Esc lost the saved expansion of " + name
    preview.capture(terminal, output, "expansions-restored", args)
    proxy.collapse_all(terminal, words)


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
            expanded_restoration(terminal, proxy, preview, output, words, args)
            proxy.open_group(terminal, "AUTO")
            terminal.key(b"\x1b[C")
            terminal.settle()
            assert re.search(r"›\s*HK 02", terminal.screen.text()), "Right from the heading did not focus the group's current node"
            for index, (width, height) in enumerate(((160, 40), (80, 24), (60, 18), (120, 18), (160, 40))):
                terminal.resize(width, height)
                terminal.settle()
                narrow = width < proxy.SIDEBAR_MIN_WIDTH
                if width == 80:
                    terminal.key(b"\x1b")
                    terminal.settle()
                    proxy.open_group(terminal, "PROXY")
                    proxy.enter_reference(terminal, "AUTO", words["proxies.enter"])
                    terminal.wait("HK 02")
                    assert words["proxies.back"] in terminal.screen.text(), "reference fixture did not retain its return path"
                if narrow:
                    assert words["proxies.search_group"] % "AUTO" in terminal.screen.text(), "narrow group label disappeared"
                terminal.key(b"\x1b[H")
                terminal.settle()
                before = view_context(terminal, proxy, words)
                first = visible_groups(terminal, proxy)[0]
                if not narrow:
                    assert words["proxies.search_group"] % first not in terminal.screen.text(), "visible wide heading has a duplicate group label"
                first_y = proxy.group_header(terminal, first)[1]
                open_search(terminal, proxy, words, "slash" if index % 2 == 0 else "click")
                assert proxy.group_header(terminal, first)[1] == first_y + 1, "proxy search occupies more than one compact row"
                preview.capture(terminal, output, "search-empty-%dx%d" % (width, height), args)
                paste_query(terminal, "hk 02")
                if narrow:
                    assert visible_groups(terminal, proxy) == ["PROXY", "AUTO", "Hong Kong"], "narrow search did not find matching groups outside the original member view"
                    assert not proxy.candidate_order(terminal, words["proxies.enter"]), "narrow directory unexpectedly displayed node cards"
                else:
                    assert set(proxy.candidate_order(terminal, words["proxies.enter"])) == {"HK 02"}, "search lost its match or retained unrelated nodes"
                terminal.key(b"\r")
                terminal.settle()
                assert "hk 02" in terminal.screen.text(), "Enter discarded node search results"
                expected_focus = "PROXY" if narrow else "HK 02"
                assert re.search(r"›\s*" + re.escape(expected_focus) + r"\b", terminal.screen.text()), "Enter did not focus the first matching group or node"
                preview.capture(terminal, output, "search-%dx%d" % (width, height), args)
                if narrow:
                    terminal.key(b"\r")
                    terminal.wait("HK 02")
                    terminal.settle()
                    assert set(proxy.candidate_order(terminal, words["proxies.enter"])) == {"HK 02"}, "entering a narrow matching group lost its filtered node"
                    preview.capture(terminal, output, "search-entered-%dx%d" % (width, height), args)
                escape_restores(terminal, proxy, words, before, "hk 02")
                preview.capture(terminal, output, "results-restored-%dx%d" % (width, height), args)
                open_search(terminal, proxy, words, "click" if index % 2 == 0 else "slash")
                paste_query(terminal, "q1sti")
                terminal.wait(words["proxies.search_no_match"])
                assert not terminal.closed, "search text triggered a global shortcut"
                preview.capture(terminal, output, "no-match-%dx%d" % (width, height), args)
                escape_restores(terminal, proxy, words, before, "q1sti")
                preview.capture(terminal, output, "members-%dx%d" % (width, height), args)
                if width == 80:
                    terminal.key(b"\x1b")
                    terminal.settle()
                    assert words["proxies.search_group"] % "PROXY" in terminal.screen.text(), "search cancellation lost the original reference history"
                    assert "US 01" in proxy.candidate_order(terminal, words["proxies.enter"]), "reference Back did not restore the original PROXY members"
                    terminal.key(b"\x1b")
                    terminal.settle()
                    proxy.open_group(terminal, "AUTO")
            terminal.exit()
            calls = json.loads(report.read_text())
            assert calls["selections"] == calls["modes"] == calls["tests"] == 0, calls
            print("PASS %s: click/slash entry, group/node filters, Enter keeps results, nonempty Esc from editing/results restores scroll/focus/expansion, current-node focus, no-match shortcut isolation, pinned search with a sticky group, one-row compact resize; no backend mutations" % language, flush=True)
        except BaseException:
            preview.capture(terminal, output, "failed", args)
            raise
        finally:
            terminal.cleanup()


if __name__ == "__main__":
    main()
