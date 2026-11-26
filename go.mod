module github.com/nelvko/lazyclash

go 1.26.0

// Keep the CJK repaint fix reproducible until Ultraviolet includes it upstream.
replace github.com/charmbracelet/ultraviolet => ./third_party/ultraviolet

require (
	github.com/charmbracelet/x/ansi v0.11.10
	github.com/charmbracelet/x/term v0.2.2
	github.com/coder/websocket v1.8.15
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/spf13/cobra v1.10.2
	golang.org/x/sys v0.50.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/nicksnyder/go-i18n/v2 v2.7.0

require github.com/charmbracelet/harmonica v0.2.0 // indirect

require (
	charm.land/bubbles/v2 v2.2.2
	charm.land/bubbletea/v2 v2.1.2
	charm.land/lipgloss/v2 v2.1.1
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/charmbracelet/colorprofile v0.4.3
	github.com/charmbracelet/ultraviolet v0.0.0-20261008173134-6b8d4baf91b4
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/mango v0.2.0 // indirect
	github.com/muesli/mango-cobra v1.3.0
	github.com/muesli/mango-pflag v0.2.0 // indirect
	github.com/muesli/roff v0.1.0
	github.com/rivo/uniseg v0.4.8-0.20261110175422-3a370411ca06
	github.com/spf13/pflag v1.1.0
	github.com/xo/terminfo v1.3.1 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/text v0.44.0
)
