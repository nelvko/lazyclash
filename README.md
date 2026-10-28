# lazyclash

A TUI-first Mihomo manager. Go module: `github.com/nelvko/lazyclash`.

First milestone: install → validate/save a Mihomo YAML subscription → apply → start → inspect groups and select a node. A valid DIRECT/REJECT default works without a subscription.

## Design decisions

- One application layer serves Bubble Tea and thin Cobra commands. The TUI never launches the lazyclash CLI.
- Linux/systemd **user** service `lazyclash-mihomo.service`: no root required for local proxy ports or API. Linger is diagnosed; enabling it is an explicit administrator/user decision. No macOS service backend, TUN, DNS or routing management.
- Fixed official Mihomo release, checksum verification, plus `install --bin` for offline and reproducible installs. No legacy aliases, migration or proxyctl configuration reads.
- Separate terminal interaction, use cases, private filesystem storage, Mihomo integration and systemd operations. No plugin/kernel framework.
- Downloads and validation are cancelable. Mutations lock before reading state. Private staging, atomic replacement and rollback protect existing files.

The adjacent retired proxyctl source was studied read-only for failure cases. It is not part of this repository or its history.

## Development

Requires Go 1.25+. `go build ./cmd/lazyclash`, `go test ./...`, `go vet ./...`.
Ordinary builds/tests never provision a VM or install a service.

See [roadmap](docs/roadmap.md). Integration verification instructions will be added with the runnable milestone.
