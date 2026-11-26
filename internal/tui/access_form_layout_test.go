package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestAccessHeaderAlignsPlainTitleWithBody(t *testing.T) {
	m := Model{width: 80, height: 24}
	for _, width := range []int{36, 74} {
		surface := m.modalSurfaceAt(hitRegion{W: width, H: 6}, modalBackground, false)
		title, phase := "编辑代理端口", "审阅变更"
		surface.accessHeader(title, phase, modalBodyInset)
		line := ansi.Strip(surface.rows[1])
		if !strings.HasPrefix(line, strings.Repeat(" ", modalBodyInset)+title) || !strings.HasSuffix(strings.TrimRight(line, " "), phase) {
			t.Fatalf("width %d: expected a title aligned with the body and a trailing phase, got %q", width, line)
		}
	}
}

func TestAccessDialogsKeepTheirAppearanceOnMouseMotion(t *testing.T) {
	for _, entry := range []string{"runtime-settings", "settings.proxyAuth", "settings.externalControl"} {
		t.Run(entry, func(t *testing.T) {
			m := focusedPortsEntry(&fakeBackend{snapshot: settingsTestSnapshot()}, 160, 45)
			m, cmd := click(m, findTarget(t, m, entry))
			m, _ = update(m, actionCmd(t, cmd)())
			if m.ports.step != "edit" {
				t.Fatalf("expected the edit form, got %q", m.ports.step)
			}
			before, focus := strings.Split(m.screen, "\n"), m.ports.focus
			for _, hit := range slices.Clone(m.targets) {
				m, _ = update(m, tea.MouseMotionMsg{X: hit.X, Y: hit.Y})
				after := strings.Split(m.screen, "\n")
				for y := hit.Y; y < hit.Y+hit.H; y++ {
					if ansi.Cut(before[y], hit.X, hit.X+hit.W) != ansi.Cut(after[y], hit.X, hit.X+hit.W) {
						t.Fatalf("hover changed the appearance of %s", hit.ID)
					}
				}
				if m.ports.focus != focus {
					t.Fatalf("hovering %s moved focus from %s to %s", hit.ID, focus, m.ports.focus)
				}
			}
		})
	}
}

func TestAccessPortFormAlignsFiveRowsAtAllSizes(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m.locale = i18n.New(language)
		for _, size := range []struct{ width, screenWidth, height, fieldRows int }{
			{30, 38, 16, 1}, {30, 38, 45, 1}, {50, 160, 24, 1}, {54, 160, 45, 3}, {54, 200, 46, 3},
		} {
			m.width, m.height = size.screenWidth, size.height
			d, _, _ := m.portsDocument(size.width)
			lineWith := func(needle string) int {
				return slices.IndexFunc(d.lines, func(line string) bool { return strings.Contains(ansi.Strip(line), needle) })
			}
			standard, transparent := lineWith(m.tr("access.standard")), lineWith(m.tr("access.transparent"))
			if standard < 0 || transparent <= standard {
				t.Fatalf("%s %v: missing or misordered port groups", language, size)
			}
			previousEnd := standard + 1
			var column int
			for i, id := range proxyFields {
				hit := findTarget(t, Model{targets: d.targets}, id)
				if i == 0 {
					column = hit.X
				}
				if hit.X != column || hit.X+hit.W != size.width || hit.W > 20 || hit.InputWidth < 5 || hit.H != size.fieldRows {
					t.Fatalf("%s %v: expected a short right-aligned %s field, got %+v", language, size, id, hit)
				}
				if hit.Y < previousEnd || (i < 3 && hit.Y+hit.H > transparent) || (i >= 3 && hit.Y <= transparent) {
					t.Fatalf("%s %v: %s overlaps the previous row or falls outside its group: %+v", language, size, id, hit)
				}
				previousEnd = hit.Y + hit.H
				label := m.tr(accessKey(id))
				labelY := lineWith(label)
				if labelY != hit.Y+hit.H/2 {
					t.Fatalf("%s %v: %s does not sit beside its field", language, size, label)
				}
				left := ansi.Strip(ansi.Cut(d.lines[labelY], 0, hit.X))
				caption := strings.TrimSpace(left)
				labelX := strings.Index(left, caption)
				if (size.fieldRows == 3 && labelX != 2) || (size.fieldRows == 1 && labelX != 0) {
					t.Fatalf("%s %v: incorrect %s label inset: %d", language, size, id, labelX)
				}
				want := label
				if id != "ports.http" {
					want += " " + tooltipIcon
					icon := findTarget(t, Model{targets: d.targets}, tooltipPrefix+id)
					if icon.Y != labelY || icon.X != labelX+ansi.StringWidth(label)+1 || icon.tip.owner != id || icon.tip.text == "" {
						t.Fatalf("%s %v: misplaced or missing tooltip for %s: %+v", language, size, label, icon)
					}
				}
				if caption != want {
					t.Fatalf("%s %v: expected %q beside the field, got %q", language, size, want, caption)
				}
				edges := ansi.Strip(ansi.Cut(d.lines[hit.Y+hit.H/2], hit.X, hit.X+hit.W))
				if !strings.HasPrefix(edges, "│") || !strings.HasSuffix(edges, "│") {
					t.Fatalf("%s %v: field edges do not match the hit target: %q", language, size, edges)
				}
			}
		}
	}
}

func TestAccessControllerFormGroupsAndResponsiveTargets(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.ports.kind = "control"
	m.initAccessDraft()
	for _, width := range []int{30, 72} {
		d, _, _ := m.portsDocument(width)
		view := Model{targets: d.targets}
		address, secret := findTarget(t, view, "ports.address"), findTarget(t, view, "ports.secret")
		if (address.Y == secret.Y) != (width == 72) {
			t.Fatalf("width %d: controller address and API secret did not reflow", width)
		}
		for _, id := range []string{"ports.tlsSection", "ports.corsSection", "ports.advancedSection"} {
			if hit := findTarget(t, view, id); hit.X != 0 || hit.W != width {
				t.Fatalf("width %d: %s disclosure did not cover its full row", width, id)
			}
		}
	}
	m.ports.access.sections["ports.corsSection"] = true
	d, _, _ := m.portsDocument(30)
	hit := findTarget(t, Model{targets: d.targets}, "ports.origins")
	if hit.H != 5 || ansi.StringWidth(d.lines[hit.Y]) != hit.W {
		t.Fatalf("CORS editor frame does not surround its three text rows: %+v", hit)
	}
}

func TestProxyPortTooltipUsesModalFocusAndPointer(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: settingsTestSnapshot()})
	m.locale = i18n.New(i18n.Chinese)
	m.layout()
	id := "ports.mixed"
	icon := findTarget(t, m, tooltipPrefix+id)
	if icon.tip.text != "同时支持 HTTP(S) 和 SOCKS5 代理协议。" || slices.Contains(m.ports.controls, icon.ID) || hasAction(m.targets, tooltipPrefix+"ports.http") {
		t.Fatalf("unexpected tooltip binding or focus stops: %+v, %v", icon, m.ports.controls)
	}
	m, _ = update(m, tea.MouseMotionMsg{X: icon.X, Y: icon.Y})
	if m.tooltip.open != "" || m.tooltip.pointer.ID != icon.ID {
		t.Fatal("hovering the icon should arm the common tooltip delay")
	}
	m, _ = update(m, m.tooltip.generation)
	if m.tooltip.bounds.W == 0 || !strings.Contains(ansi.Strip(m.screen), icon.tip.text) {
		t.Fatal("hover did not show an explanation above the modal")
	}
	m, _ = update(m, tea.MouseMotionMsg{X: icon.X - 1, Y: icon.Y})
	m, _ = click(m, icon)
	if m.tooltip.open != "" || m.ports.focus != id {
		t.Fatal("leaving or clicking an icon changed form focus or left the explanation open")
	}
	m, _ = update(m, key("?"))
	if m.tooltip.bounds.W == 0 || !m.tooltip.keyboard {
		t.Fatal("question mark did not show the focused port's tooltip")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.tooltip.open != "" || !m.ports.editingProxyPorts() {
		t.Fatal("Escape should close only the explanation")
	}
	m, _ = click(m, findTarget(t, m, "ports.http"))
	m, _ = update(m, key("?"))
	if m.tooltip.open != "" || m.ports.field("ports.http").Value() != "" {
		t.Fatal("HTTP has no explanation and should not receive the shortcut as input")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.ports.open || m.tooltip.focused(id).ID != "" {
		t.Fatal("closing the modal must drop its tooltip anchors")
	}
}

func TestProxyPortTooltipHidesCursorOverItsText(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: settingsTestSnapshot()})
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 24})
	m, _ = click(m, findTarget(t, m, "ports.http"))
	cursor := m.inputCursor()
	icon := findTarget(t, m, tooltipPrefix+"ports.mixed")
	m, _ = update(m, tea.MouseMotionMsg{X: icon.X, Y: icon.Y})
	m, _ = update(m, m.tooltip.generation)
	if cursor == nil || !regionsOverlap(m.tooltip.bounds, hitRegion{X: cursor.X, Y: cursor.Y, W: 1, H: 1}) {
		t.Fatal("expected a tooltip over the active HTTP input")
	}
	if m.inputCursor() != nil {
		t.Fatal("the hardware cursor should stay behind the tooltip")
	}
}

func TestProxyPortTooltipExplainsUnknownPlatform(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: settingsTestSnapshot()})
	for _, platform := range []string{"windows", "darwin", "linux", "android", ""} {
		m.ports.current.CoreOS = platform
		m.layout()
		for _, id := range []string{"ports.redir", "ports.tproxy"} {
			icon := tooltipAnchor(m.targets, tooltipPrefix+id)
			if platform == "windows" || platform == "darwin" && id == "ports.tproxy" {
				if icon.ID != "" {
					t.Errorf("%s: hidden %s still has a tooltip", platform, id)
				}
				continue
			}
			key := accessKey(id) + ".tooltip"
			if platform == "" {
				key += ".unknownPlatform"
			}
			if icon.tip.text != m.tr(key) {
				t.Errorf("%s: incorrect %s explanation: %q", platform, id, icon.tip.text)
			}
		}
	}
}

func TestProxyPortTooltipRevealsFieldAfterScrolling(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: settingsTestSnapshot()})
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 13})
	m.ports.viewport.GotoBottom()
	m.renderLayout()
	if m.tooltip.offscreen.tip.owner != m.ports.focus {
		t.Fatal("fixture should have scrolled the focused port out of view")
	}
	m, _ = update(m, key("?"))
	if m.tooltip.bounds.W == 0 || m.tooltip.offscreen.ID != "" {
		t.Fatal("keyboard explanation did not reveal its focused field")
	}
	assertCanvas(t, m.screen, m.width, m.height)
}
