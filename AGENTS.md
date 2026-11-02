# Repository Guidelines

## Project Structure & Module Organization

lazyclash is a Go 1.26+ Mihomo proxy manager with a TUI-first interface.

- `main.go`: executable entry point.
- `internal/cli/` and `internal/tui/`: Cobra commands and Bubble Tea interaction, both calling shared `internal/app/` use cases. Do not invoke the CLI from the TUI.
- `internal/configuration/`: Profile metadata and snapshots; `internal/mihomo/`: releases, configuration building, and control API.
- `internal/service/`, `internal/downloadnet/`, `internal/store/`: systemd user services, download networking, and atomic storage/locking.
- Tests live beside source as `*_test.go`. `scripts/` contains development and verification tools; `docs/` explains architecture and behavior. Generated binaries and diagnostics belong in ignored `bin/` and `.test-artifacts/`.

## Build, Test, and Development Commands

- `make build`: build `bin/lazyclash`; `go run .`: run locally.
- `make test`: run all Go tests.
- `make check`: run Go tests and `go vet ./...`.
- `make race`: run tests with the race detector.
- `python3 -m unittest discover -s scripts -p 'test_*.py'`: test terminal-screen parsing.
- `./scripts/dev.sh VM_NAME`: cross-build and enter an existing dedicated OrbStack Linux VM.

## Coding Style & Naming Conventions

Use `gofmt` with tab indentation. Follow Go naming conventions: exported `MixedCase`, unexported `mixedCase`, and descriptive `snake_case.go` filenames. Keep business logic in `internal/app/`, independent of Cobra and Bubble Tea. Use the existing `charm.land/*/v2` dependencies.

## Testing Guidelines

Use Go's standard `testing` package, descriptive `TestBehavior` names, table-driven cases, `t.TempDir()`, local HTTP fixtures, and fake services. Cover cancellation, stale results, rollback, and credential redaction where relevant; no numeric coverage threshold is configured.

Standalone core integration requires `LAZYCLASH_TEST_MIHOMO=/absolute/path/to/mihomo`. For Linux changes, explicitly run `./scripts/verify-linux.sh "$VM"`; real Mihomo/systemd/PTY verification uses `./scripts/e2e.sh "$VM"`. Read `docs/verification.md` first. Cross-compilation alone is not runtime verification.

## Commit & Pull Request Guidelines

Follow existing Conventional Commits: `feat(tui): add local profile path completion`, `test(settings): ...`, or `chore: ...`. Keep subjects imperative and commits focused.

PRs should explain behavior changes, link relevant issues, and list verification results and limitations. Include terminal screenshots and tested dimensions for TUI changes. Update affected documentation.

## Security & Configuration

Service installation supports Linux/systemd user services, not macOS. Use isolated data directories and dedicated service names for runtime experiments. Never commit subscription credentials, API secrets, raw profiles, or unredacted logs.

## Agent Instructions

@/Users/nelvko/.codex/RTK.md

Prefer suitable Charm Bubbles components for TUI work; implement and encapsulate custom components only when none fits.
