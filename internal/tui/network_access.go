package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

const (
	bindAddressID     = "ports.bindAddress"
	allowedNetworksID = "ports.allowedNetworks"
	deniedNetworksID  = "ports.deniedNetworks"
)

func bindAddressSummary(s app.RuntimeSettings, locale i18n.Localizer) string {
	address := s.ConfiguredBindAddress()
	if s.AllowLAN && address == "*" {
		return locale.T("settings.allAddresses")
	}
	return address
}

func accessRulesSummary(s app.RuntimeSettings, locale i18n.Localizer) string {
	allowed, denied := strings.Fields(s.LANAllowedIPs), strings.Fields(s.LANDisallowedIPs)
	if len(allowed) == 0 {
		return locale.T("access.denyAll")
	}
	summary := locale.T("access.allowCount", len(allowed))
	if slices.Contains(allowed, "0.0.0.0/0") && slices.Contains(allowed, "::/0") {
		summary = locale.T("access.allowAll")
	}
	if len(denied) > 0 {
		summary += " · " + locale.T("access.denyCount", len(denied))
	}
	return summary
}

func (m *Model) initNetworkAccess() {
	p := &m.ports
	p.focus = p.firstField()
	if p.kind == "bind" {
		v := textinput.New()
		v.SetVirtualCursor(false)
		v.SetValue(p.current.ConfiguredBindAddress())
		p.access.inputs[bindAddressID] = &v
		return
	}
	p.access.areas = map[string]*textarea.Model{
		allowedNetworksID: newAccessTextarea(p.current.LANAllowedIPs, "192.168.1.0/24", 3),
		deniedNetworksID:  newAccessTextarea(p.current.LANDisallowedIPs, "192.168.1.50/32", 2),
	}
}

func (m *Model) resetNetworkRules() {
	p := &m.ports
	p.textarea(allowedNetworksID).SetValue(mihomo.DefaultLANAllowedIPs)
	p.textarea(deniedNetworksID).Reset()
	p.focus, p.reveal, p.err = allowedNetworksID, true, ""
}

// Start with the loaded settings. Equivalent formatting in a field must not
// turn an inherited value into an instance override.
func (m *Model) networkAccessProposal() (app.RuntimeSettings, map[string]error) {
	p := &m.ports
	s := p.current
	issues := make(map[string]error)
	if p.kind == "bind" {
		address, err := mihomo.NormalizeBindAddress(p.field(bindAddressID).Value())
		if err != nil {
			issues[bindAddressID] = err
		} else if address != s.ConfiguredBindAddress() {
			s.BindAddress = address
		}
		return s, issues
	}
	for _, field := range []struct {
		id    string
		value *string
	}{{allowedNetworksID, &s.LANAllowedIPs}, {deniedNetworksID, &s.LANDisallowedIPs}} {
		normalized, err := mihomo.NormalizePrefixes(p.textarea(field.id).Value())
		if err != nil {
			issues[field.id] = err
		} else {
			current, err := mihomo.NormalizePrefixes(*field.value)
			if err != nil || current != normalized {
				*field.value = normalized
			}
		}
	}
	return s, issues
}

func (m *Model) drawNetworkAccess(d *wizardDocument) {
	p := &m.ports
	next, issues := m.networkAccessProposal()
	d.disabled = func(id string) bool { return id == "ports.review" && (len(issues) > 0 || next == p.current) }
	if p.kind == "bind" {
		d.label(m.tr("settings.bindAddress") + "  " + themed("bind-address", muted, modalBackground, false))
		input := p.field(bindAddressID)
		input.Err = issues[bindAddressID]
		appendAccessFields(d, 0, m.accessInputBlock(bindAddressID, d.width, m.height < 22))
		m.drawBindChoices(d)
		if input.Err != nil {
			d.line(themed(m.localizeError(input.Err), errorColor, modalBackground, false))
		}
		d.space()
		hint := "access.bindHint"
		if len(p.access.bindMenu.items) == 0 {
			hint = "access.bindManualHint"
		}
		d.line(themed(m.tr(hint), muted, modalBackground, false))
		return
	}
	d.line(themed(m.tr("access.rulesHint"), muted, modalBackground, false))
	compact := d.width < 40 && m.height <= 24
	allowHeight := 3
	if compact {
		allowHeight = 2
	}
	if area := p.textarea(allowedNetworksID); area.Height() != allowHeight {
		area.SetHeight(allowHeight)
	}
	for _, field := range []struct{ id, key string }{
		{allowedNetworksID, "lan-allowed-ips"}, {deniedNetworksID, "lan-disallowed-ips"},
	} {
		if !compact || field.id == deniedNetworksID {
			d.space()
		}
		d.label(m.tr(accessKey(field.id)) + "  " + themed(field.key, muted, modalBackground, false))
		err := issues[field.id]
		m.accessTextarea(d, field.id, err != nil)
		if err != nil {
			d.line(themed(m.localizeError(err), errorColor, modalBackground, false))
		} else if field.id == allowedNetworksID {
			if strings.TrimSpace(p.textarea(field.id).Value()) == "" {
				d.line(themed(m.tr("access.emptyAllowWarning"), warning, modalBackground, false))
			} else {
				d.line(themed(m.tr("access.defaultAllowHint"), muted, modalBackground, false))
			}
		}
		if p.focus == field.id {
			d.focusedRowEnd = len(d.lines)
		}
	}
}

func (m *Model) reviewNetworkAccess(d *wizardDocument) {
	p := &m.ports
	before, after := p.plan.Current, p.plan.Proposed
	if p.kind == "bind" {
		d.accessReviewRow(m.tr("settings.bindAddress"), before.ConfiguredBindAddress()+" → "+after.ConfiguredBindAddress())
		return
	}
	for _, field := range []struct{ id, old, next string }{
		{allowedNetworksID, before.LANAllowedIPs, after.LANAllowedIPs},
		{deniedNetworksID, before.LANDisallowedIPs, after.LANDisallowedIPs},
	} {
		if field.old != field.next {
			show := func(s string) string {
				if s != "" {
					return strings.Join(strings.Fields(s), ", ")
				}
				if field.id == allowedNetworksID {
					return m.tr("access.denyAll")
				}
				return m.tr("access.unset")
			}
			d.accessReviewRow(m.tr(accessKey(field.id)), show(field.old)+" → "+show(field.next))
		}
	}
}
