# Local Ultraviolet patch

This directory contains the complete Go source and tests from
`github.com/charmbracelet/ultraviolet`
`v0.0.0-20261008173134-6b8d4baf91b4`, the version already selected by
lazyclash. The original MIT license is retained in `LICENSE`.

The root module replaces this dependency with the repository copy so ordinary
`go build`, `go run`, packaging, and Linux development all use the same fix.

## Patch

`terminal_renderer.go`, `paintLine`: skip an identical row before taking the
wide-character repaint branch. A forced repaint or a row damaged by scrolling
or resizing must still be painted. Changed wide rows retain the upstream
erase, autowrap protection, and cursor anchoring.

Bubble Tea clears and draws its cell buffer for each changed frame, marking
unchanged Chinese and emoji rows as touched. Upstream repaints every touched
wide row, which exposes blank horizontal strips on terminals without active
synchronized output, including the reported Termius session.

Upstream report: https://github.com/charmbracelet/ultraviolet/issues/163

Regression coverage is in `internal/tui/renderer_regression_test.go`.
`make test`, `make check`, `make race`, and `scripts/verify-linux.sh` include
the copied upstream tests, including scrolling and resize regressions.

When updating Ultraviolet, compare the new source with this pinned version,
retain or replace this patch, and run those checks. Remove the replacement
and this directory once an upstream version passes the regression tests
without the patch.
