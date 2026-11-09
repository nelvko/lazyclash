# Repository Guidelines

## Project Structure & Module Organization

lazyclash is a Go 1.26+ Mihomo proxy manager with a TUI-first interface.

- `main.go`: executable entry point.
- `internal/cli/` and `internal/tui/`: Cobra commands and Bubble Tea interaction, both calling shared `internal/app/` use cases. Do not invoke the CLI from the TUI.
- `internal/configuration/`: Profile metadata and snapshots; `internal/mihomo/`: releases, configuration building, and control API.
- `internal/service/`, `internal/downloadnet/`, `internal/store/`: systemd user/system services and finite TUN lifecycle hooks, download networking, and scope-specific atomic storage/locking.
- Tests live beside source as `*_test.go`. `scripts/` contains development and verification tools; `docs/` explains architecture and behavior. Generated binaries and diagnostics belong in ignored `bin/` and `.test-artifacts/`.

## Build, Test, and Development Commands

- `make build`: build `bin/lazyclash`; `go run .`: run locally.
- `make package VERSION=dev`: produce Linux amd64/arm64 archives and SHA256SUMS in `bin/packages/`.
- `make test`: run all Go tests.
- `make check`: run Go tests and `go vet ./...`.
- `make race`: run tests with the race detector.
- `python3 -m unittest discover -s scripts -p 'test_*.py'`: test terminal-screen parsing.
- `./scripts/dev.sh VM_NAME`: enter an existing dedicated OrbStack Linux VM; each `lazyclash` invocation cross-builds the latest code on macOS before running.

## Coding Style & Naming Conventions

Use `gofmt` with tab indentation. Follow Go naming conventions: exported `MixedCase`, unexported `mixedCase`, and descriptive `snake_case.go` filenames. Keep business logic in `internal/app/`, independent of Cobra and Bubble Tea. Use the existing `charm.land/*/v2` dependencies.

## Testing Guidelines

Use Go's standard `testing` package, descriptive `TestBehavior` names, table-driven cases, `t.TempDir()`, local HTTP fixtures, and fake services. Cover cancellation, stale results, rollback, and credential redaction where relevant; no numeric coverage threshold is configured.

Standalone core integration requires `LAZYCLASH_TEST_MIHOMO=/absolute/path/to/mihomo`. For Linux changes, explicitly run `./scripts/verify-linux.sh "$VM"`; user Mihomo/systemd/PTY verification uses `./scripts/e2e.sh "$VM"`. Dual-scope and host TUN verification uses `./scripts/service-scopes-e2e.sh "$VM" /absolute/packaged/lazyclash /absolute/vm/mihomo`. Read `docs/verification.md` first. Cross-compilation alone is not runtime verification.

## Commit & Pull Request Guidelines

Follow existing Conventional Commits: `feat(tui): add local profile path completion`, `test(settings): ...`, or `chore: ...`. Keep subjects imperative and commits focused.

PRs should explain behavior changes, link relevant issues, and list verification results and limitations. Include terminal screenshots and tested dimensions for TUI changes. Update affected documentation.

## Security & Configuration

Service installation supports Linux/systemd user and system services, not macOS. Ordinary users manage only their user proxy; root manages an independent system instance under `/var/lib/lazyclash/<service-name>`, with a dedicated non-root Mihomo identity. `sudo lazyclash` never copies or switches user data. Host-only TUN requires explicit confirmation; ordinary system proxying has no extra capabilities, while TUN grants only `CAP_NET_ADMIN` and `CAP_NET_RAW`. Units directly start Mihomo and invoke finite lifecycle commands from the same protected lazyclash executable; there is no helper or daemon. Interrupted management journals recover on the next invocation. Do not add legacy compatibility or automatic migration.

Use isolated data directories and dedicated service names for runtime experiments. Never commit subscription credentials, API secrets, raw profiles, or unredacted logs.

## Agent Instructions

@/Users/nelvko/.codex/RTK.md

Prefer suitable Charm Bubbles components for TUI work; implement and encapsulate custom components only when none fits.

遵循 karpathy-guidelines，但需注意：

1. 架构设计优先考虑正确性、可维护性和明确的扩展需求。
2. 不为假设性的未来需求过度设计。
3. 对已确定的未来需求，允许合理抽象和预留扩展点。
4. 项目处于早期开发阶段，不以兼容旧实现或最小 Diff 为首要目标。
5. 发现现有设计不合理时，主动提出重构或重写方案。
6. 实现完成后必须验证功能，而不是仅保证代码能编译。
