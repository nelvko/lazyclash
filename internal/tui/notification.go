package tui

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
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
	Silent                               bool
	showAfter                            time.Time
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
	details               string
	critical              bool
	warning               bool
	retainDialog          bool
	overlays              []notificationOverlay
	panel                 notificationPanel
	restoreFocus          string
}

func (m *Model) notificationSource() notificationSource {
	return notificationSource{m.id, m.notification.operation.ID, m.message, m.failure, m.busy, m.messageTransient, m.messageInformational, m.page == "overview" && m.inlineInstallVisible()}
}

func (m *Model) feedbackDialogOpen() bool {
	return m.ports.open || m.wizard.open || m.wizard.networkOpen || m.form != ""
}

func (m *Model) prepareNotification(key, context string) {
	m.startNotification(notificationOperation{Key: key, Context: context})
}

func (m *Model) startNotification(op notificationOperation) {
	n := &m.notification
	n.nextOperation++
	op.ID, op.Page, op.Dialog = n.nextOperation, m.page, m.feedbackDialogOpen()
	n.operation = op
	key := op.Key
	if m.wizard.inline && (key == "check-update" || key == "resolve" || key == "prepare" || key == "install") {
		n.operation.Inline = true
	}
	n.details, n.critical, n.warning = "", false, false
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
func (m *Model) syncNotification() {
	n := &m.notification
	source := m.notificationSource()
	if source == n.source {
		return
	}
	n.source = source
	op := n.operation
	index := n.itemIndex(op.ID, op.Key)
	// An already published operation must reach its result even after its owner
	// moves back into view. New ordinary feedback belongs to that owner.
	if op.Inline && source.inlineVisible && !n.critical && index < 0 {
		return
	}
	retain := n.retainDialog || (n.critical && source.failure != "")
	if ((m.feedbackDialogOpen() || n.operation.Dialog) && !retain) || (source.failure == "" && (source.message == "" || (source.transient && index < 0))) || (source.busy && m.job == "refresh" && index < 0) {
		return
	}
	kind, message := "done", source.message
	if source.busy {
		kind = "working"
	} else if source.failure != "" {
		kind, message = "error", source.failure
	} else if n.warning {
		kind = "warning"
	} else if source.informational {
		kind = "info"
	}
	if source.busy && op.ID == n.dismissedOperation && op.ID != 0 {
		return
	}
	if op.Page == "" {
		op.Page = m.page
	}
	item := notificationItem{Operation: op.ID, Key: op.Key, Page: op.Page, Context: op.Context, Message: message, Kind: kind, Critical: n.critical}
	if source.busy && n.retainDialog {
		item.Critical = true
	}
	item.Silent = !item.Critical && (kind == "info" && op.Key != geoMaintenanceID || kind == "done" && !op.announcesSuccess(m.page))
	if kind == "working" && !item.Critical {
		item.showAfter = time.Now().Add(500 * time.Millisecond)
		if index >= 0 && n.items[index].Kind == "working" {
			item.showAfter = n.items[index].showAfter
		}
	}
	if kind == "error" {
		item.Details = n.details
	}
	n.publish(item, index)
}

func (n *notificationState) itemIndex(operation uint64, key string) int {
	for i, item := range n.items {
		if operation != 0 && item.Operation == operation {
			return i
		}
		if key != "" && item.Key == key && (item.Kind == "error" || item.Kind == "warning") {
			return i
		}
	}
	return -1
}

// Foreground tasks and independent preference saves share identities, retry
// replacement and expiry. Publishing a background warning never changes the job.
func (n *notificationState) publish(item notificationItem, index int) {
	n.generation++
	item.generation = n.generation
	switch item.Kind {
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
		if n.items[index].Operation != item.Operation {
			n.items = append(append(n.items[:index:index], n.items[index+1:]...), item)
		} else {
			n.items[index] = item
		}
	}
}

// Visible cards and quiet records consume their display time. Overflow and
// modal views pause the timer and retain the remaining display time.
func (m *Model) syncNotificationTimers(now time.Time) tea.Cmd {
	n := &m.notification
	if len(n.items) == 0 {
		return nil
	}
	visible := map[uint64]bool{}
	for _, overlay := range n.overlays {
		for _, id := range overlay.IDs {
			visible[id] = true
		}
	}
	obscured := m.notificationsObscured()
	var commands []tea.Cmd
	for i := range n.items {
		item := &n.items[i]
		if item.remaining <= 0 {
			continue
		}
		running := !obscured && (item.Silent || visible[item.ID])
		if !running && !item.started.IsZero() {
			item.remaining = max(time.Nanosecond, item.remaining-now.Sub(item.started))
			item.started = time.Time{}
			n.generation++
			item.generation = n.generation
		}
		if running && item.started.IsZero() {
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
		if item.ID == msg.id && item.generation == msg.generation && !item.started.IsZero() {
			m.dismissNotificationID(item.ID)
			return
		}
	}
}

// A locally visible success still resolves the earlier warning or error.
func (m *Model) resolveNotificationIssue(key string) {
	n := &m.notification
	if index := n.itemIndex(0, key); index >= 0 {
		m.dismissNotificationID(n.items[index].ID)
	}
}

func (m *Model) dismissNotificationID(id uint64) {
	n := &m.notification
	for i, item := range n.items {
		if item.ID != id {
			continue
		}
		n.items = append(n.items[:i:i], n.items[i+1:]...)
		if n.panel.open {
			switch {
			case len(n.items) == 0:
				m.notificationAction("notification.panel.close")
			case n.panel.detailID == id:
				m.notificationAction(n.panel.backAction())
			}
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
	if strings.HasPrefix(m.focus, "notification.") && strings.HasSuffix(m.focus, "."+strconv.FormatUint(id, 10)) {
		m.restoreNotificationFocus()
	}
}

func (m *Model) hasNotificationControl(action string) bool {
	return slices.ContainsFunc(m.targets, func(hit hitRegion) bool { return hit.ID == action })
}

func (m *Model) dismissFocusedNotification() {
	if m.hasNotificationControl(m.focus) {
		switch control, rawID, _ := strings.Cut(strings.TrimPrefix(m.focus, "notification."), "."); control {
		case "close", "details", "cancel":
			if id, err := strconv.ParseUint(rawID, 10, 64); err == nil {
				m.dismissNotificationID(id)
				return
			}
		}
	}
	m.restoreNotificationFocus()
}

func (m *Model) restoreNotificationFocus() {
	m.focus = m.notification.restoreFocus
	if m.focus == "" || strings.HasPrefix(m.focus, "notification.") {
		m.focus = "nav." + m.page
	}
	if !m.sidebarOpen && !sidebarVisible(m.width, m.height) && strings.HasPrefix(m.focus, "nav.") {
		m.focus = "menu"
	}
}

func (m *Model) openNotifications(id uint64) {
	n := &m.notification
	for _, item := range n.items {
		if id == 0 || item.ID == id {
			if !n.panel.open && !strings.HasPrefix(m.focus, "notification.") {
				n.restoreFocus = m.focus
			}
			n.panel.show(id)
			m.focus, m.hover = "notification.panel", ""
			return
		}
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
		m.openNotifications(0)
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
			if prefix == "notification.close." || prefix == "notification.panel.dismiss." {
				m.dismissNotificationID(id)
			} else if m.hasNotificationControl(action) {
				m.openNotifications(id)
			}
			return true
		}
	}
	return true
}

func (m *Model) notificationsObscured() bool {
	return m.notification.panel.open || m.dashboardDimmed() || m.sidebarOpen || m.menu.open || (m.page == "connections" && m.connections.detail != nil)
}

func (m *Model) monochrome() bool {
	return m.noColor || m.colorProfile == colorprofile.ASCII || m.colorProfile == colorprofile.NoTTY
}

func (m *Model) dashboardOptions() dashboardOptions {
	items := m.notification.items
	if m.notificationsObscured() {
		items = nil
	} else if m.page == "overview" && m.inlineInstallVisible() {
		items = nil
		for _, item := range m.notification.items {
			if item.Operation != m.notification.operation.ID || !m.notification.operation.Inline || item.Critical {
				items = append(items, item)
			}
		}
	}
	o := dashboardOptions{settingsTab: m.settingsTab, Locale: m.locale, Width: m.width, Height: m.height, Page: m.page, Busy: m.busy, PlainButtons: m.buttonPlain(), Dim: m.dashboardDimmed(), SidebarOpen: m.sidebarOpen, HideKeyHints: m.keyHints.hidden || m.wizard.open || m.wizard.networkOpen, GuideSkipped: m.guideSkipped, Notifications: items, Spinner: m.spinner.View(), LogoFrame: m.logoFrame, LogoIntroFrame: m.logoIntro.frame, InlineInstall: m.page == "overview" && m.inlineInstallVisible(), BodyRange: m.page == "proxies" && m.proxiesReady() && !m.proxiesDirect() && m.hasExpandedProxyGroups()}
	o.KeyHintsPending = m.keyHints.pending
	if m.busy {
		o.PendingOperation = m.notification.operation.Key
	}
	o.Now = time.Now()
	o.geoMaintenanceBusy = o.PendingOperation == geoMaintenanceID
	if m.coreNumber.id != "" {
		o.coreNumber = &m.coreNumber
	}
	o.SelectedGroup = cmp.Or(m.overviewGroup, m.proxies.activeGroup)
	o.Theme, o.EffectiveTheme = m.theme, m.effectiveTheme()
	o.StartPage = m.startPage
	o.EditorCommand = m.editorPreference
	o.DownloadMethod = m.wizard.cfg.Method
	o.TrafficHistory, o.TrafficNow = m.runtime.history.points, o.Now
	o.ipInfo = m.ipInfo
	o.websiteTests, o.websites = m.websiteTests, m.websites
	o.websiteDrag = m.websiteDrag
	o.Monochrome = m.monochrome()
	if m.menu.open && m.menu.dropdown != nil {
		o.OpenDropdown = m.menu.anchor.ID
	}
	if strings.HasPrefix(o.PendingOperation, "service.") {
		o.CoreAction = strings.TrimPrefix(o.PendingOperation, "service.")
	}
	if len(o.TrafficHistory) > 0 {
		o.TrafficScale = m.runtime.chartScale.current
	}
	if m.page == "rules" || m.page == "logs" {
		g := dashboardGeometry(o)
		rows := m.filterToolbarRows(g.width)
		if g.height >= rows+2 {
			o.BodyTopRows = rows
		}
	}
	if m.page == "proxies" {
		o.BodyTopRows = m.proxyFixedRows(o)
	}
	if m.page == "connections" {
		g := dashboardGeometry(o)
		rows := m.connectionToolbarRows(g.width)
		if g.height >= rows+2 {
			o.BodyTopRows = rows
		}
	}
	return o
}
