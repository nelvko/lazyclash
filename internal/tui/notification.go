package tui

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type notificationExpiredMsg struct{ id, generation uint64 }

// Keys identify an operation and resource, never its result text. Hash resources
// so credential-bearing subscription addresses cannot leak into UI identifiers.
func notificationKey(operation, resource string) string {
	return fmt.Sprintf("%s:%x", operation, sha256.Sum256([]byte(resource)))
}

type notificationOperation struct {
	ID                 uint64
	Key, Page, Context string
	Dialog             bool
	Inline             bool
}

type notificationItem struct {
	ID, Operation                        uint64
	Key, Page, Context, Message, Details string
	Kind                                 string
	Critical                             bool
	generation                           uint64
	remaining                            time.Duration
	started                              time.Time
}

type notificationSource struct {
	id, event        uint64
	message, failure string
	busy             bool
	transient        bool
	informational    bool
	inlineVisible    bool
}

type notificationState struct {
	source                notificationSource
	operation             notificationOperation
	items                 []notificationItem
	nextID, nextOperation uint64
	dismissedOperation    uint64
	generation            uint64
	visible               bool
	details               string
	critical              bool
	retainDialog          bool
	panel                 notificationPanel
	restoreFocus          string
}

func (m Model) notificationSource() notificationSource {
	return notificationSource{m.id, m.notification.operation.ID, m.message, m.failure, m.busy, m.messageTransient, m.messageInformational, m.page == "overview" && m.inlineInstallVisible()}
}

func (m Model) feedbackDialogOpen() bool {
	return m.ports.open || m.wizard.open || m.wizard.networkOpen || m.form != ""
}

func (m *Model) prepareNotification(key, context string) {
	n := &m.notification
	n.nextOperation++
	n.operation = notificationOperation{ID: n.nextOperation, Key: key, Page: m.page, Context: context, Dialog: m.feedbackDialogOpen()}
	if m.wizard.inline && (key == "check-update" || key == "resolve" || key == "prepare" || key == "install") {
		n.operation.Inline = true
	}
	n.details, n.critical = "", false
	n.retainDialog = false
	for _, item := range n.items {
		if item.Key == key && item.Critical {
			n.retainDialog = true
			break
		}
	}
}

// Existing notifications survive page changes, later operations and dialogs;
// dialog-owned results are never replayed as global feedback.
func (m *Model) syncNotification() tea.Cmd {
	n := &m.notification
	source := m.notificationSource()
	if source == n.source {
		return nil
	}
	n.source = source
	op := n.operation
	if op.Inline && source.inlineVisible && !n.critical {
		return nil
	}
	index := -1
	for i, item := range n.items {
		if item.Operation == op.ID || (op.Key != "" && item.Key == op.Key && item.Kind == "error") {
			index = i
			break
		}
	}
	retain := n.retainDialog || (n.critical && source.failure != "")
	if ((m.feedbackDialogOpen() || n.operation.Dialog) && !retain) || (source.failure == "" && (source.message == "" || (source.transient && index < 0))) || (source.busy && m.job == "refresh" && index < 0) {
		return nil
	}
	kind, message := "done", source.message
	if source.busy {
		kind = "working"
	} else if source.failure != "" {
		kind, message = "error", source.failure
	} else if source.informational {
		kind = "info"
	}
	if source.busy && op.ID == n.dismissedOperation && op.ID != 0 {
		return nil
	}
	if op.Page == "" {
		op.Page = m.page
	}
	n.generation++
	item := notificationItem{Operation: op.ID, Key: op.Key, Page: op.Page, Context: op.Context, Message: message, Kind: kind, Critical: n.critical, generation: n.generation}
	if source.busy && n.retainDialog {
		item.Critical = true
	}
	if kind == "error" {
		item.Details = n.details
		if item.Details == "" {
			item.Details = message
		}
		if ansi.StringWidth(clean(message)) > 200 {
			item.Message = m.tr("notification.failure_details")
		}
	}
	switch kind {
	case "done":
		item.remaining = 3 * time.Second
	case "info":
		item.remaining = 4 * time.Second
	}
	if index < 0 {
		n.nextID++
		item.ID = n.nextID
		n.items = append(n.items, item)
	} else {
		item.ID = n.items[index].ID
		// A retry is a new arrival, but still replaces the original failure.
		if n.items[index].Operation != op.ID {
			n.items = append(append(n.items[:index:index], n.items[index+1:]...), item)
		} else {
			n.items[index] = item
		}
	}
	n.visible = len(n.items) > 0
	return nil
}

// Only fully expanded, unobscured cards consume their display time. Overflow
// and modal views pause the timer and retain the remaining display time.
func (m *Model) syncNotificationTimers(now time.Time) tea.Cmd {
	n := &m.notification
	if len(n.items) == 0 {
		return nil
	}
	visible := map[uint64]bool{}
	if !n.panel.open && !m.menu.open {
		o := m.dashboardOptions()
		o.Scroll = m.scroll
		m.populatePageBody(&o)
		for _, overlay := range notificationLayout(o) {
			for _, id := range overlay.IDs {
				visible[id] = true
			}
		}
	}
	var commands []tea.Cmd
	for i := range n.items {
		item := &n.items[i]
		if item.remaining <= 0 {
			continue
		}
		if !visible[item.ID] && !item.started.IsZero() {
			item.remaining = max(time.Nanosecond, item.remaining-now.Sub(item.started))
			item.started = time.Time{}
			n.generation++
			item.generation = n.generation
		}
		if visible[item.ID] && item.started.IsZero() {
			item.started = now
			n.generation++
			item.generation = n.generation
			id, generation := item.ID, item.generation
			commands = append(commands, tea.Tick(item.remaining, func(time.Time) tea.Msg {
				return notificationExpiredMsg{id: id, generation: generation}
			}))
		}
	}
	return tea.Batch(commands...)
}

func (m *Model) expireNotification(msg notificationExpiredMsg) {
	for _, item := range m.notification.items {
		if item.ID == msg.id && item.generation == msg.generation && !item.started.IsZero() && item.Kind != "error" && item.Kind != "working" {
			m.dismissNotificationID(item.ID)
			return
		}
	}
}

func (m *Model) dismissNotificationID(id uint64) {
	n := &m.notification
	for i, item := range n.items {
		if item.ID != id {
			continue
		}
		n.items = append(n.items[:i:i], n.items[i+1:]...)
		if n.panel.open && n.panel.detailID == id {
			n.panel.show(0)
		}
		if item.Operation == n.operation.ID {
			n.dismissedOperation = item.Operation
			if !m.busy {
				m.message, m.failure = "", ""
				m.messageTransient, m.messageInformational = false, false
			}
			n.source = m.notificationSource()
		}
		break
	}
	n.generation++
	n.visible = len(n.items) > 0
	if strings.HasPrefix(m.focus, "notification.") && strings.HasSuffix(m.focus, "."+strconv.FormatUint(id, 10)) {
		m.restoreNotificationFocus()
	}
}

func (m *Model) dismissNotification() {
	if items := m.notification.items; len(items) > 0 {
		m.dismissNotificationID(items[len(items)-1].ID)
	}
}

func (m *Model) restoreNotificationFocus() {
	m.focus = m.notification.restoreFocus
	if m.focus == "" || strings.HasPrefix(m.focus, "notification.") {
		m.focus = "nav." + m.page
	}
	if (m.width < 100 || m.height < 20) && strings.HasPrefix(m.focus, "nav.") {
		m.focus = "menu"
	}
}

func (m *Model) notificationAction(action string) bool {
	if !strings.HasPrefix(action, "notification.") {
		return false
	}
	n := &m.notification
	if strings.HasPrefix(action, "notification.cancel.") {
		id, err := strconv.ParseUint(strings.TrimPrefix(action, "notification.cancel."), 10, 64)
		if err == nil && m.busy {
			for _, item := range n.items {
				if item.ID == id && item.Operation == n.operation.ID && item.Kind == "working" {
					m.cancelTask(false)
					break
				}
			}
		}
		return true
	}
	switch action {
	case "notification.expand":
		if !n.panel.open && !strings.HasPrefix(m.focus, "notification.") {
			n.restoreFocus = m.focus
		}
		n.panel.show(0)
	case "notification.panel.close":
		n.panel.dismiss()
		m.restoreNotificationFocus()
	case "notification.panel.back":
		n.panel.show(0)
	default:
		for _, prefix := range []string{"notification.close.", "notification.details.", "notification.panel.dismiss.", "notification.panel.details."} {
			if !strings.HasPrefix(action, prefix) {
				continue
			}
			id, err := strconv.ParseUint(strings.TrimPrefix(action, prefix), 10, 64)
			if err != nil {
				return true
			}
			if strings.Contains(prefix, "details") {
				if !n.panel.open && !strings.HasPrefix(m.focus, "notification.") {
					n.restoreFocus = m.focus
				}
				n.panel.show(id)
			} else {
				m.dismissNotificationID(id)
			}
			return true
		}
	}
	return true
}

func (m Model) dashboardOptions() dashboardOptions {
	items := m.notification.items
	if m.page == "overview" && m.inlineInstallVisible() {
		items = nil
		for _, item := range m.notification.items {
			if item.Operation != m.notification.operation.ID || !m.notification.operation.Inline || item.Critical {
				items = append(items, item)
			}
		}
	}
	o := dashboardOptions{Locale: m.locale, Width: m.width, Height: m.height, Page: m.page, Busy: m.busy, PlainButtons: m.buttonPlain(), Dim: m.feedbackDialogOpen() || m.helpOpen || m.notification.panel.open, SidebarOpen: m.sidebarOpen, HideKeyHints: m.keyHints.hidden || m.wizard.open || m.wizard.networkOpen, GuideSkipped: m.guideSkipped, Notifications: items, Spinner: m.spinner.View(), LogoFrame: m.logoFrame, LogoIntroFrame: m.logoIntro.frame, InlineInstall: m.page == "overview" && m.inlineInstallVisible(), BodyRange: m.page == "proxies" && m.proxiesReady() && m.hasExpandedProxyGroups() && m.proxies.detail == ""}
	if m.page == "rules" && m.rules.detail == nil || m.page == "logs" {
		g := dashboardGeometry(o)
		if g.height >= 6 {
			o.BodyTopRows = 3
			if g.width < 68 {
				o.BodyTopRows++
			}
		}
	}
	if m.page == "connections" && m.connections.detail == nil {
		g := dashboardGeometry(o)
		rows := m.connectionToolbarRows(g.width)
		if g.height >= rows+2 {
			o.BodyTopRows = rows
		}
	}
	return o
}

// Single-card adapter for geometry callers outside the dashboard.
func notificationToast(o dashboardOptions) (string, hitRegion, hitRegion) {
	overlays := notificationLayout(o)
	if len(overlays) == 0 {
		return "", hitRegion{}, hitRegion{}
	}
	first := overlays[0]
	var close hitRegion
	for _, target := range first.Targets {
		if strings.HasPrefix(target.ID, "notification.close.") {
			close = target
			break
		}
	}
	return first.Content, first.Bounds, close
}
