"""Shared PTY geometry; keep these thresholds aligned with dashboard.go."""

SIDEBAR_MIN_WIDTH = 115
SIDEBAR_MIN_HEIGHT = 9


def sidebar_visible(columns, rows):
    return columns >= SIDEBAR_MIN_WIDTH and rows >= SIDEBAR_MIN_HEIGHT
