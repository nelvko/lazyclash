"""Shared TUI layout checks for screen captures from a real PTY."""
import re
import time

SIDEBAR_MIN_WIDTH = 115
SIDEBAR_MIN_HEIGHT = 9


def sidebar_visible(columns, rows):
    return columns >= SIDEBAR_MIN_WIDTH and rows >= SIDEBAR_MIN_HEIGHT


def _wait_dropdown_text(terminal, pattern):
    deadline = time.monotonic() + 10
    while True:
        if not terminal.screen.synchronized and terminal.screen.state == "text":
            match = re.search(pattern, terminal.screen.text())
            if match:
                return match["value"]
        if terminal.closed or time.monotonic() >= deadline:
            raise AssertionError("Missing dropdown text: " + pattern + "\n" + terminal.screen.text())
        terminal.read()


def dropdown_trigger(terminal, label, expanded=False):
    arrow = "▴" if expanded else "▾"
    return _wait_dropdown_text(terminal, r"(?P<value>" + re.escape(label) + " +" + arrow + ")")


def dropdown_option(terminal, label, checked=False):
    # The left marker is present only for the active row in monochrome.
    suffix = r" +✓ *│" if checked else r" +│"
    return _wait_dropdown_text(terminal, r"│[ ›]*(?P<value>" + re.escape(label) + suffix + ")")
