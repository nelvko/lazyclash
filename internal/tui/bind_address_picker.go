package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

const (
	bindMenuID       = "ports.bindMenu"
	bindAllID        = "ports.bindAll"
	bindOptionPrefix = "ports.bindOption."
	// The option frame, cursor, checkmark and their padding surround each label.
	bindChoiceGutter = 7
)

func (p *portsState) bindDialogWidth() int {
	// Leave room for a bracketed IPv6 address in the editor beside its controls.
	width := 60
	for _, item := range p.access.bindMenu.items {
		width = max(width, ansi.StringWidth(item.label())+bindChoiceGutter+2*modalBodyInset)
	}
	return width
}

func (m *Model) initBindMenu(addresses []app.BindAddressOption) {
	if len(addresses) == 0 {
		return
	}
	menu := &m.ports.access.bindMenu
	menu.items = []menuItem{{ID: "*", Label: m.tr("access.allInterfaces")}}
	for _, addr := range addresses {
		menu.items = append(menu.items, menuItem{ID: addr.Address, Label: addr.Address + "  " + addr.Interface})
	}
	m.openBindMenu()
}

func (m *Model) openBindMenu() {
	p := &m.ports
	menu := &p.access.bindMenu
	value, _ := mihomo.NormalizeBindAddress(p.field(bindAddressID).Value())
	menu.open, menu.selected, menu.offset = len(menu.items) != 0, -1, 0
	for i := range menu.items {
		menu.items[i].Checked = menu.items[i].ID == value
		if menu.items[i].Checked {
			menu.selected = i
		}
	}
	p.focus, p.reveal = bindAddressID, true
}

func (m *Model) pickBindAddress(address string) {
	p := &m.ports
	p.field(bindAddressID).SetValue(address)
	p.access.bindMenu.dismiss()
	p.focus, p.reveal, p.err = bindAddressID, true, ""
}

func (m *Model) activateBindControl(id string) bool {
	menu := &m.ports.access.bindMenu
	switch {
	case id == bindMenuID && menu.open:
		menu.dismiss()
		m.ports.focus = bindAddressID
	case id == bindMenuID || id == bindAddressID:
		m.openBindMenu()
	case id == bindAllID:
		m.pickBindAddress("*")
	case strings.HasPrefix(id, bindOptionPrefix):
		m.pickBindAddress(strings.TrimPrefix(id, bindOptionPrefix))
	default:
		return false
	}
	return true
}

func (m *Model) bindMenuKey(key string) bool {
	p := &m.ports
	menu := &p.access.bindMenu
	if p.focus != bindAddressID || len(menu.items) == 0 {
		return false
	}
	switch key {
	case "down", "up":
		if !menu.open {
			m.openBindMenu()
		} else if key == "down" {
			menu.move(1)
		} else {
			menu.move(-1)
		}
		p.reveal = true
	case "enter", "esc":
		if !menu.open {
			return false
		}
		if key == "enter" && menu.selected >= 0 {
			m.pickBindAddress(menu.items[menu.selected].ID)
		} else {
			menu.dismiss()
		}
	default:
		menu.dismiss()
		return false // Typing and normal focus navigation stay with the editor.
	}
	return true
}

func (m *Model) drawBindChoices(d *wizardDocument) {
	p := &m.ports
	menu := &p.access.bindMenu
	if len(menu.items) == 0 {
		d.space()
		d.control(bindAllID, m.tr("access.allInterfaces"), p.focus)
		return
	}
	if !menu.open {
		return
	}
	labels := make([][]string, len(menu.items))
	for i, item := range menu.items {
		labels[i] = strings.Split(ansi.Wrap(item.label(), d.width-bindChoiceGutter, ":"), "\n")
	}
	end := menu.fitBindChoices(labels, max(2, min(5, m.height-14)))
	top := len(d.lines)
	d.lines = append(d.lines, menu.edge(d.width, true, menu.offset > 0))
	edge := themed("│", border, raised, false)
	for i := menu.offset; i < end; i++ {
		item := menu.items[i]
		d.targets = append(d.targets, hitRegion{ID: bindOptionPrefix + item.ID, Y: len(d.lines), W: d.width, H: len(labels[i])})
		for line, label := range labels[i] {
			item.Label = label
			row := (dropdown{ShowCursor: true}).row(item, d.width-2, i == menu.selected, d.plain, line)
			d.lines = append(d.lines, edge+row+edge)
		}
		if i == menu.selected {
			d.focusedRowEnd = len(d.lines)
		}
	}
	d.lines = append(d.lines, menu.edge(d.width, false, end < len(menu.items)))
	d.optionList = hitRegion{Y: top, W: d.width, H: len(d.lines) - top}
}

// Scroll by whole options, counting wrapped lines against the available height.
func (menu *actionMenu) fitBindChoices(labels [][]string, height int) int {
	first := max(0, min(menu.offset, menu.selected))
	end, used := first, 0
	for end < len(labels) {
		if end > first && used+len(labels[end]) > height {
			if end > menu.selected {
				break
			}
			used -= len(labels[first])
			first++
			continue
		}
		used += len(labels[end])
		end++
	}
	for first > 0 && used+len(labels[first-1]) <= height {
		first--
		used += len(labels[first])
	}
	menu.offset = first
	return end
}
