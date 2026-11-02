package tui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
)

const (
	maxPathEntries = 4096
	maxPathMatches = 200
)

type pathMatch struct {
	path, label string
	directory   bool
}

type pathMatchesMsg struct {
	generation uint64
	query      string
	matches    []pathMatch
	limited    bool
	problem    string
}

// Bubbles textinput owns editing. This companion supplies asynchronous local
// directory matches and a bounded list, which its inline suggestions cannot.
type pathCompletion struct {
	generation uint64
	query      string
	matches    []pathMatch
	index      int
	active     bool
	loading    bool
	limited    bool
	problem    string
}

func (p *pathCompletion) reset() {
	*p = pathCompletion{generation: p.generation + 1}
}

func (p *pathCompletion) request(value string) tea.Cmd {
	value = strings.TrimSpace(value)
	if p.query == value && p.active {
		return nil
	}
	p.reset()
	if value == "" || strings.Contains(value, "://") {
		return nil
	}
	p.query, p.active, p.loading = value, true, true
	generation := p.generation
	return func() tea.Msg {
		result := readPathMatches(value)
		result.generation = generation
		return result
	}
}

func (p *pathCompletion) update(msg pathMatchesMsg) bool {
	if !p.active || p.generation != msg.generation || p.query != msg.query {
		return false
	}
	p.matches, p.limited, p.problem = msg.matches, msg.limited, msg.problem
	p.loading, p.index = false, 0
	return true
}

func (p *pathCompletion) move(delta int) {
	if len(p.matches) > 0 {
		p.index = (p.index + delta%len(p.matches) + len(p.matches)) % len(p.matches)
	}
}

func (p pathCompletion) selectedPath() (string, bool) {
	if !p.active || p.loading || p.index < 0 || p.index >= len(p.matches) {
		return "", false
	}
	return p.matches[p.index].path, true
}

func (p pathCompletion) view(width, rows int, locales ...i18n.Localizer) []string {
	var locale i18n.Localizer
	if len(locales) > 0 {
		locale = locales[0]
	}
	if !p.active || width < 1 || rows < 1 {
		return nil
	}
	status := ""
	switch {
	case p.loading:
		status = locale.T("path.finding")
	case p.problem != "":
		status = locale.Detail(p.problem)
	case len(p.matches) == 0:
		status = locale.T("path.no_matches")
	}
	if status != "" {
		return []string{themed(cellCrop(status, width), muted, raised, false)}
	}
	count := min(rows, len(p.matches))
	showCount := rows > 1 && (len(p.matches) > rows || p.limited)
	if showCount {
		count = min(rows-1, len(p.matches))
	}
	start := max(0, p.index-count+1)
	lines := make([]string, 0, rows)
	for i := start; i < min(start+count, len(p.matches)); i++ {
		match := p.matches[i]
		prefix, foreground, background := "  ", text, raised
		if match.directory {
			foreground = accent
		}
		if i == p.index {
			prefix, foreground, background = "› ", focus, selectionBackground
		}
		lines = append(lines, themed(cellPad(prefix+match.label, width), foreground, background, false))
	}
	if showCount {
		label := locale.T("path.matches", len(p.matches))
		if p.limited {
			label = locale.T("path.more_entries")
		}
		lines = append(lines, themed(cellCrop(label, width), muted, raised, false))
	}
	return lines
}

func resolveLocalPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value, nil
	}
	if value == "" {
		return "", diagnostic.New("diagnostic.path_required", "enter a local file path")
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", diagnostic.New("diagnostic.home_not_found", "cannot locate your home directory")
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~"))
	} else if strings.HasPrefix(value, "~") {
		return "", diagnostic.New("diagnostic.home_syntax", "use ~/ for your home directory")
	}
	return filepath.Abs(value)
}

func readPathMatches(value string) pathMatchesMsg {
	result := pathMatchesMsg{query: value}
	if value == "~" {
		value = "~/"
	}
	prefix, name := filepath.Split(value)
	dir, err := resolveLocalPath(prefix)
	if prefix == "" {
		dir, err = filepath.Abs(".")
	}
	if err != nil {
		result.problem = err.Error()
		return result
	}
	if strings.HasPrefix(value, "~") && !strings.HasPrefix(value, "~/") {
		result.problem = "Use ~/ for your home directory"
		return result
	}
	file, err := os.Open(dir)
	if err != nil {
		result.problem = pathReadProblem(err)
		return result
	}
	defer file.Close()
	var scanned int
	for scanned < maxPathEntries {
		entries, readErr := file.ReadDir(min(256, maxPathEntries-scanned))
		scanned += len(entries)
		for _, entry := range entries {
			label := entry.Name()
			if !strings.HasPrefix(label, name) || (!strings.HasPrefix(name, ".") && strings.HasPrefix(label, ".")) || strings.ContainsFunc(label, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
				continue
			}
			// The import store accepts ordinary files; special entries and
			// symlinks are not offered as configurations to select.
			if !entry.IsDir() && !entry.Type().IsRegular() {
				continue
			}
			if entry.IsDir() {
				label += string(filepath.Separator)
			}
			result.matches = append(result.matches, pathMatch{path: prefix + label, label: label, directory: entry.IsDir()})
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				result.problem = pathReadProblem(readErr)
				result.matches = nil
			}
			break
		}
	}
	result.limited = scanned == maxPathEntries
	sort.Slice(result.matches, func(i, j int) bool {
		if result.matches[i].directory != result.matches[j].directory {
			return result.matches[i].directory
		}
		return result.matches[i].label < result.matches[j].label
	})
	if len(result.matches) > maxPathMatches {
		result.matches, result.limited = result.matches[:maxPathMatches], true
	}
	return result
}

func pathReadProblem(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "Directory not found"
	}
	return "Cannot read this directory"
}
