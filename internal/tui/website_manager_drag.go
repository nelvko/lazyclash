package tui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

const siteDragGrip = "websiteManager.drag."
const siteManagerRowID = "websiteManager.site."

type siteDragState struct {
	source, neighbor app.Website
	startX, startY   int
	after            bool
	tick             *siteDragScrollMsg
}

func (d siteDragState) active() bool { return d.source.URL != "" }

func (m *Model) cancelSiteDrag() { m.siteManager.drag = siteDragState{} }

// A fresh pointer identifies each tick, including visits to the same edge.
type siteDragScrollMsg struct{ step int }

func (m *Model) siteDragMouse(event tea.MouseMsg) (bool, tea.Cmd) {
	p := event.Mouse()
	d := &m.siteManager.drag
	switch event.(type) {
	case tea.MouseClickMsg:
		// An independent press also ends a gesture whose release was lost.
		m.cancelSiteDrag()
		if m.form != "website-manager" || m.busy || m.quitting || p.Button != tea.MouseLeft {
			return false, nil
		}
		index, grip, _ := m.siteDragHit(p)
		if !grip || index < 0 {
			return false, nil
		}
		m.selectManagerSite(index)
		m.hover = ""
		*d = siteDragState{source: m.websites[index], neighbor: m.websites[index], startX: p.X, startY: p.Y}
		return true, nil
	case tea.MouseMotionMsg:
		if !d.active() {
			return false, nil
		}
		if p.Button != tea.MouseLeft {
			m.cancelSiteDrag()
			return false, nil
		}
		index, _, edge := m.siteDragHit(p)
		d.neighbor = app.Website{}
		if index >= 0 {
			d.neighbor = m.websites[index]
			d.after = index > m.siteManager.selected
		}
		if p.X == d.startX && p.Y == d.startY && d.neighbor == d.source {
			edge = 0
		}
		return true, d.scheduleScroll(edge)
	case tea.MouseReleaseMsg:
		if !d.active() {
			return false, nil
		}
		index, _, _ := m.siteDragHit(p)
		source := d.source
		m.cancelSiteDrag()
		if p.Button != tea.MouseLeft || index < 0 || source == m.websites[index] {
			return true, nil
		}
		return true, m.relocateManagerSite(source, m.websites[index], index > m.siteManager.selected)
	case tea.MouseWheelMsg:
		m.cancelSiteDrag()
	}
	return false, nil
}

// Only rendered list rows are drop targets; the footer, padding and modal
// background intentionally have no drag semantics.
func (m *Model) siteDragHit(p tea.Mouse) (index int, grip bool, edge int) {
	var url string
	visible := 0
	for _, hit := range m.targets {
		if strings.HasPrefix(hit.ID, siteManagerRowID) {
			visible++
		}
		if !regionsOverlap(hit, hitRegion{X: p.X, Y: p.Y, W: 1, H: 1}) {
			continue
		}
		if rowURL, ok := strings.CutPrefix(hit.ID, siteManagerRowID); ok {
			url = rowURL
		} else if gripURL, ok := strings.CutPrefix(hit.ID, siteDragGrip); ok {
			url, grip = gripURL, true
		}
	}
	if url == "" {
		return -1, false, 0
	}
	index = slices.IndexFunc(m.websites, func(s app.Website) bool { return s.URL == url })
	if index > 0 && index == m.siteManager.offset {
		edge = -1
	} else if index >= 0 && index+1 < len(m.websites) && index == m.siteManager.offset+visible-1 {
		edge = 1
	}
	return index, grip, edge
}

func (d *siteDragState) scheduleScroll(step int) tea.Cmd {
	if step == 0 {
		d.tick = nil
		return nil
	}
	if d.tick != nil && d.tick.step == step {
		return nil
	}
	msg := &siteDragScrollMsg{step: step}
	d.tick = msg
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return msg })
}

func (m *Model) scrollSiteDrag(msg *siteDragScrollMsg) tea.Cmd {
	d := &m.siteManager.drag
	if m.form != "website-manager" || m.busy || m.quitting || msg == nil || msg != d.tick {
		return nil
	}
	d.tick = nil
	index := slices.Index(m.websites, d.neighbor)
	next := index + msg.step
	if index < 0 || next < 0 || next >= len(m.websites) {
		return nil
	}
	m.siteManager.offset += msg.step
	d.neighbor, d.after = m.websites[next], next > m.siteManager.selected
	if next+msg.step < 0 || next+msg.step >= len(m.websites) {
		return nil
	}
	return d.scheduleScroll(msg.step)
}
