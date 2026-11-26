package tui

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// Each ID appears in the editor and in the same order on the review screen.
var proxyFields = []string{"ports.mixed", "ports.http", "ports.socks", "ports.redir", "ports.tproxy"}
var controllerFields = []string{"ports.address", "ports.secret", "ports.https", "ports.cert", "ports.key", "ports.origins", "ports.private", "ports.unix", "ports.mark", "ports.doh"}

type accessDraft struct {
	inputs                 map[string]*textinput.Model
	sections               map[string]bool
	origins                textarea.Model
	private                bool
	auth                   proxyAuthDraft
	bindField, bindMessage string // Last socket probe failure, cleared when its field changes.
}

func (p *portsState) visibleProxyFields() []string {
	if !p.current.SupportsRedirect() {
		return proxyFields[:3]
	}
	if !p.current.SupportsTProxy() {
		return proxyFields[:4]
	}
	return proxyFields
}

func (p *portsState) proxyPortPlatformKnown() bool {
	switch p.current.CoreOS {
	case "linux", "android", "darwin", "windows":
		return true
	}
	return false
}

func (p *portsState) accessEditor() bool {
	return p.kind == "proxy" || p.kind == "control" || p.kind == "auth"
}

func (p *portsState) firstField() string {
	if p.kind == "auth" {
		return p.access.auth.firstField()
	}
	if p.kind == "control" {
		return "ports.address"
	}
	return "ports.mixed"
}

func (p *portsState) field(id string) *textinput.Model {
	if p.kind == "auth" {
		return p.access.auth.field(id)
	}
	if id == "ports.mixed" {
		return &p.mixed
	}
	return p.access.inputs[id]
}

func accessKey(id string) string { return "access." + strings.TrimPrefix(id, "ports.") }

func accessControllerAddress(s app.RuntimeSettings) string {
	host := s.ControllerHost
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(s.ControllerPort))
}

func accessValues(s app.RuntimeSettings) map[string]string {
	return map[string]string{
		"ports.mixed": optionalPort(s.MixedPort), "ports.http": optionalPort(s.HTTPPort),
		"ports.socks": optionalPort(s.SOCKSPort), "ports.redir": optionalPort(s.RedirPort),
		"ports.tproxy": optionalPort(s.TProxyPort), "ports.address": accessControllerAddress(s),
		"ports.https": s.HTTPSAddress, "ports.cert": s.CertificatePath, "ports.key": s.PrivateKeyPath,
		"ports.origins": s.AllowedOrigins, "ports.private": strconv.FormatBool(s.AllowPrivateNetwork),
		"ports.unix": s.UnixSocket, "ports.mark": strconv.FormatUint(uint64(s.RoutingMark), 10), "ports.doh": s.DoHPath,
	}
}

func optionalPort(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
}

func (m *Model) initAccessDraft() {
	p := &m.ports
	fields := p.visibleProxyFields()
	if p.kind == "control" {
		fields = controllerFields
	}
	p.access = accessDraft{inputs: make(map[string]*textinput.Model), sections: make(map[string]bool), private: p.current.AllowPrivateNetwork}
	values := accessValues(p.current)
	for _, id := range fields {
		if id == "ports.private" || id == "ports.origins" {
			continue
		}
		input := p.field(id)
		if input == nil {
			v := textinput.New()
			v.SetVirtualCursor(false)
			input = &v
			p.access.inputs[id] = input
		}
		input.SetValue(values[id])
		if p.kind == "proxy" {
			input.CharLimit = 0 // Validate the complete pasted value.
			input.Placeholder = m.tr("access.disabled")
		}
		if id == "ports.secret" {
			input.EchoMode, input.EchoCharacter = textinput.EchoPassword, '•'
			input.Placeholder = m.tr("access.secretPlaceholder")
		}
	}
	if p.kind == "control" {
		v := textarea.New()
		v.ShowLineNumbers, v.Prompt, v.MaxHeight, v.MaxWidth = false, "", 0, 0
		v.Placeholder = m.tr("access.originHint")
		v.SetHeight(3)
		v.SetValue(p.current.AllowedOrigins)
		v.SetVirtualCursor(true)
		p.access.origins = v
	}
	p.focus = p.firstField()
}

func (m *Model) invalidPortField(id, reason string) (app.RuntimeSettings, bool) {
	p := &m.ports
	p.err, p.focus, p.reveal = reason, id, true
	if input := p.field(id); input != nil {
		input.Err = errors.New(reason)
	}
	return app.RuntimeSettings{}, false
}

// Validation is authoritative in the backend. Reopen a collapsed section when
// its diagnostic identifies the field that needs attention.
func (m *Model) revealControlError(err error) {
	var de *diagnostic.Error
	if m.ports.kind != "control" || !errors.As(err, &de) {
		return
	}
	p := &m.ports
	var field, section string
	switch de.Code {
	case "diagnostic.settings.controllerTLS":
		field, section = "ports.cert", "ports.tlsSection"
		if p.field(field).Value() != "" {
			field = "ports.key"
		}
	case "diagnostic.settings.dohPath":
		field, section = "ports.doh", "ports.advancedSection"
	case "diagnostic.settings.corsOrigin":
		field, section = "ports.origins", "ports.corsSection"
	case "diagnostic.config.controllerSecret":
		field = "ports.secret"
	case "diagnostic.settings.controllerLocal", "diagnostic.settings.controllerAddress":
		field = "ports.address"
		if p.field(field).Value() == accessControllerAddress(p.current) && p.field("ports.https").Value() != "" {
			field, section = "ports.https", "ports.tlsSection"
		}
	default:
		return
	}
	if section != "" {
		p.access.sections[section] = true
	}
	p.focus, p.reveal = field, true
	if v := p.field(field); v != nil {
		v.Err = err
	}
}

func (m *Model) accessProposal() (app.RuntimeSettings, bool) {
	p := &m.ports
	if p.kind == "proxy" {
		return m.proxyPortProposal()
	}
	s := p.current
	value := func(id string) string { return strings.TrimSpace(p.field(id).Value()) }
	host, port, err := net.SplitHostPort(value("ports.address"))
	number, parseErr := strconv.ParseUint(port, 10, 16)
	if err != nil || host == "" || parseErr != nil || number < 1024 {
		return m.invalidPortField("ports.address", m.tr("access.invalidAddress"))
	}
	if host == "127.0.0.1" && p.current.ControllerHost == "" {
		host = ""
	}
	s.ControllerHost, s.ControllerPort = host, int(number)
	s.NewSecret = p.field("ports.secret").Value()
	s.HTTPSAddress, s.CertificatePath, s.PrivateKeyPath = value("ports.https"), value("ports.cert"), value("ports.key")
	s.AllowedOrigins, s.AllowPrivateNetwork = p.access.origins.Value(), p.access.private
	s.UnixSocket, s.DoHPath = value("ports.unix"), value("ports.doh")
	mark := value("ports.mark")
	base := 10
	if strings.HasPrefix(strings.ToLower(mark), "0x") {
		base, mark = 16, mark[2:]
	}
	n, err := strconv.ParseUint(mark, base, 32)
	if err != nil && (mark != "" || base == 16) {
		p.access.sections["ports.advancedSection"] = true
		return m.invalidPortField("ports.mark", m.tr("access.invalidMark"))
	}
	s.RoutingMark = uint32(n)
	if s == p.current {
		p.err = m.tr("access.unchanged")
		return app.RuntimeSettings{}, false
	}
	return s, true
}

func (p *portsState) proxyFieldError(id string) string {
	if field := p.field(id); field != nil && field.Err != nil {
		return field.Err.Error()
	}
	return ""
}

// Only the ports present in this editor contribute to validation and changes.
// The proposal starts with the complete snapshot so hidden values are retained.
func (m *Model) proxyPortProposal() (app.RuntimeSettings, bool) {
	p := &m.ports
	next := p.current
	ports := map[string]*int{
		"ports.mixed": &next.MixedPort, "ports.http": &next.HTTPPort, "ports.socks": &next.SOCKSPort,
		"ports.redir": &next.RedirPort, "ports.tproxy": &next.TProxyPort,
	}
	used := make(map[int]string)
	valid := true
	for _, id := range p.visibleProxyFields() {
		field := p.field(id)
		field.Err = nil
		raw := strings.TrimSpace(field.Value())
		n, err := strconv.ParseUint(raw, 10, 16)
		if raw != "" && err != nil {
			field.Err = errors.New(m.tr("access.portRange"))
			valid = false
			continue
		}
		port := int(n)
		*ports[id] = port
		if port == 0 {
			continue
		}
		if other := used[port]; other != "" {
			field.Err = errors.New(m.tr("access.portConflict", port))
			p.field(other).Err = field.Err
		}
		used[port] = id
		listener := mihomo.ListenerEndpoint{Network: "tcp", Address: next.ProxyListenAddress(port)}
		for _, address := range []string{accessControllerAddress(next), next.HTTPSAddress} {
			if address != "" && listener.Conflicts(mihomo.ListenerEndpoint{Network: "tcp", Address: address}) {
				field.Err = errors.New(m.tr("access.portControllerConflict", port))
			}
		}
		if id == p.access.bindField {
			field.Err = errors.New(p.access.bindMessage)
		}
		valid = valid && field.Err == nil
	}
	return next, valid && next != p.current
}

func (m *Model) revealProxyBindError(err error) {
	p := &m.ports
	var de *diagnostic.Error
	if p.kind != "proxy" || !errors.As(err, &de) || len(de.Args) < 2 {
		return
	}
	if de.Code != "diagnostic.settings.portUnavailable" && de.Code != "diagnostic.settings.portPermission" {
		return
	}
	address, _ := de.Args[1].(string)
	for _, id := range p.visibleProxyFields() {
		n, parseErr := strconv.ParseUint(strings.TrimSpace(p.field(id).Value()), 10, 16)
		if parseErr == nil && n != 0 && p.current.ProxyListenAddress(int(n)) == address {
			p.access.bindField, p.access.bindMessage = id, m.localizeError(err)
			p.err, p.step, p.focus, p.reveal = "", "edit", id, true
			m.message = ""
			return
		}
	}
}

func (m *Model) accessReview(d *wizardDocument) {
	if m.ports.kind == "auth" {
		m.proxyAuthReview(d)
		return
	}
	p := &m.ports
	old, next := accessValues(p.plan.Current), accessValues(p.plan.Proposed)
	fields := p.visibleProxyFields()
	if p.kind == "control" {
		fields = controllerFields
	}
	renderValue := func(id, v string) string {
		if id == "ports.private" {
			if v == "true" {
				return m.tr("dashboard.control.on")
			}
			return m.tr("dashboard.control.off")
		}
		if v == "" {
			if p.kind == "proxy" {
				return m.tr("access.disabled")
			}
			return m.tr("access.unset")
		}
		return strings.ReplaceAll(v, "\n", ", ")
	}
	for _, id := range fields {
		if id == "ports.secret" {
			if p.plan.SecretChanged {
				d.accessReviewRow(m.tr("access.secret"), m.tr("access.secretChange"))
			}
			continue
		}
		if old[id] != next[id] {
			d.accessReviewRow(m.tr(accessKey(id)), renderValue(id, old[id])+" → "+renderValue(id, next[id]))
		}
	}
}

func (m *Model) toggleAccessLAN() tea.Cmd {
	if !m.pageSelectionMenuAvailable("settings") || !m.snapshot.Installed {
		return nil
	}
	backend := m.backend
	on := !m.snapshot.RuntimeSettings.AllowLAN
	label := m.tr("settings.allowLAN")
	return m.begin(m.tr("settings.coreSaving", label), m.tr("settings.coreSaved", label), func(ctx context.Context) error {
		current, err := backend.GetRuntimeSettings(ctx)
		if err != nil || current.AllowLAN == on {
			return err
		}
		proposed := current
		proposed.AllowLAN = on
		plan, err := backend.PrepareRuntimeSettingsEdit(ctx, current, proposed)
		if err != nil {
			return err
		}
		_, err = backend.ApplyRuntimeSettings(ctx, plan)
		return err
	}, notificationOperation{Key: "settings.allowLAN", Context: label})
}
