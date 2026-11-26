package tui

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

const proxyAuthAdd = "ports.auth.add"

type proxyAuthDraft struct {
	current app.ProxyAuthSettings
	rows    []proxyAuthRow
}

type proxyAuthRow struct {
	original           string
	username, password textinput.Model
}

func authRowID(control string, index int) string {
	return "ports.auth." + control + "." + strconv.Itoa(index)
}

func (draft *proxyAuthDraft) target(id string) (string, int) {
	tail, ok := strings.CutPrefix(id, "ports.auth.")
	if !ok {
		return "", -1
	}
	control, index, _ := strings.Cut(tail, ".")
	i, err := strconv.Atoi(index)
	if err != nil || i < 0 || i >= len(draft.rows) {
		return "", -1
	}
	return control, i
}

func (draft *proxyAuthDraft) field(id string) *textinput.Model {
	control, i := draft.target(id)
	switch control {
	case "username":
		return &draft.rows[i].username
	case "password":
		return &draft.rows[i].password
	}
	return nil
}

func (draft *proxyAuthDraft) firstField() string {
	if len(draft.rows) > 0 {
		return authRowID("username", 0)
	}
	return proxyAuthAdd
}

func (m *Model) appendProxyAuthRow(original string) {
	row := proxyAuthRow{original: original, username: textinput.New(), password: textinput.New()}
	row.username.SetValue(clean(original))
	row.password.EchoMode, row.password.EchoCharacter = textinput.EchoPassword, '•'
	if original != "" {
		row.password.Placeholder = m.tr("access.authKeepPassword")
	}
	draft := &m.ports.access.auth
	draft.rows = append(draft.rows, row)
}

func (m *Model) initProxyAuthDraft(saved app.ProxyAuthSettings) {
	draft := &m.ports.access.auth
	*draft = proxyAuthDraft{current: saved}
	for _, name := range saved.Usernames {
		m.appendProxyAuthRow(name)
	}
	m.ports.focus = draft.firstField()
}

func (m *Model) activateProxyAuth(id string) bool {
	p := &m.ports
	draft := &p.access.auth
	if id == proxyAuthAdd {
		m.appendProxyAuthRow("")
		p.focus = authRowID("username", len(draft.rows)-1)
	} else if control, i := draft.target(id); control == "remove" {
		draft.rows = slices.Delete(draft.rows, i, i+1)
		p.focus = proxyAuthAdd
		if i < len(draft.rows) {
			p.focus = authRowID("username", i)
		}
	} else {
		return false
	}
	p.reveal, p.err, p.plan = true, "", app.RuntimeSettingsPlan{}
	return true
}

func (m *Model) proxyAuthEdit(d *wizardDocument) {
	p := &m.ports
	d.line(themed(m.tr("access.authHint"), muted, modalBackground, false))
	d.space()
	if len(p.access.auth.rows) == 0 {
		d.line(m.tr("access.authEmpty"))
		d.space()
	}
	for i := range p.access.auth.rows {
		m.proxyAuthAccount(d, i)
		d.space()
	}
	d.control(proxyAuthAdd, "+ "+m.tr("access.authAdd"), p.focus)
}

func (m *Model) proxyAuthAccount(d *wizardDocument, i int) {
	const gap = 2
	removeID := authRowID("remove", i)
	remove := renderButton(m.tr("access.authRemove"), buttonOptions{
		Kind: buttonText, TextColor: buttonDangerForeground, Surface: modalBackground, Compact: true,
		Focused: m.ports.focus == removeID, Plain: d.plain,
	})
	removeWidth := ansi.StringWidth(remove)
	fieldsWidth := d.width - removeWidth - 2*gap
	nameWidth, passwordWidth := d.width, d.width
	columns := fieldsWidth >= 48
	if columns {
		nameWidth = fieldsWidth * 2 / 5
		passwordWidth = fieldsWidth - nameWidth
	}
	name := m.accessFieldBlock(authRowID("username", i), m.tr("access.authUsername"), nameWidth)
	password := m.accessFieldBlock(authRowID("password", i), m.tr("access.authPassword"), passwordWidth)
	if columns {
		// Align the action with the editable text inside the three-line frame.
		action := dashboardBlock{lines: []string{"", "", remove}, targets: []hitRegion{{ID: removeID, Y: 2, W: removeWidth, H: 1}}}
		appendAccessFields(d, gap, name, password, action)
		return
	}
	// On narrow terminals the action can share the username label line. Keep
	// it after both fields in the Tab order so entering credentials flows down.
	actionX, actionY := d.width-removeWidth, len(d.lines)
	name.lines[0] = cellPad(name.lines[0], actionX) + remove
	appendAccessFields(d, 0, name)
	appendAccessFields(d, 0, password)
	d.targets = append(d.targets, hitRegion{ID: removeID, X: actionX, Y: actionY, W: removeWidth, H: 1})
}

func (m *Model) proxyAuthUsers() ([]app.ProxyAuthUser, bool) {
	p := &m.ports
	var users []app.ProxyAuthUser
	seen := make(map[string]bool)
	for i := range p.access.auth.rows {
		row := &p.access.auth.rows[i]
		row.username.Err, row.password.Err = nil, nil
		user := app.ProxyAuthUser{OriginalUsername: row.original, Username: row.username.Value(), Password: row.password.Value()}
		err := user.Validate()
		field, message := "username", ""
		switch {
		case strings.TrimSpace(user.Username) == "":
			message = m.tr("access.authUsernameRequired")
		case err != nil:
			message = m.localizeError(err)
			if de, ok := errors.AsType[*diagnostic.Error](err); ok && de.Code == "diagnostic.settings.proxyAuthPassword" {
				field = "password"
				if user.Password == "" {
					message = m.tr("access.authPasswordRequired")
				}
			}
		case seen[user.Username]:
			message = m.tr("access.authDuplicate")
		}
		if message != "" {
			p.focus, p.err, p.reveal = authRowID(field, i), message, true
			p.field(p.focus).Err = errors.New(p.err)
			return nil, false
		}
		seen[user.Username] = true
		users = append(users, user)
	}
	return users, true
}

func (m *Model) proxyAuthReview(d *wizardDocument) {
	draft := &m.ports.access.auth
	start := len(d.lines)
	retained := make(map[string]bool)
	for _, row := range draft.rows {
		name := clean(row.username.Value())
		retained[row.original] = true
		if row.original == "" {
			d.accessReviewRow(m.tr("access.authAdded"), name)
			continue
		}
		if name != row.original {
			d.accessReviewRow(m.tr("access.authRenamed"), clean(row.original)+" → "+name)
		}
		if row.password.Value() != "" {
			d.accessReviewRow(m.tr("access.authPasswordChanged"), name)
		}
	}
	for _, name := range draft.current.Usernames {
		if !retained[name] {
			d.accessReviewRow(m.tr("access.authRemoved"), clean(name))
		}
	}
	if len(draft.rows) == 0 {
		d.line(m.tr("access.authDisabled"))
	} else if len(d.lines) == start {
		// Adopting the current profile's list can itself be the only change.
		for _, row := range draft.rows {
			d.accessReviewRow(m.tr("access.authUsername"), row.username.Value())
		}
	}
	d.space()
	d.line(themed(m.tr("access.authManagedHint"), muted, modalBackground, false))
}
