package app

import (
	"context"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// ProxyAuthSettings exposes account names and source exemptions to the editor.
// The generation identifies the exact credentials opened, including inherited ones.
type ProxyAuthSettings struct {
	Usernames        []string
	SkipAuthPrefixes string
	revision         string
}

// OriginalUsername identifies the saved password to preserve when Password is
// empty. A new account must supply a password.
type ProxyAuthUser struct {
	OriginalUsername string
	Username         string
	Password         string `json:"-"`
}

type proxyAuthEdit struct {
	revision string
	users    []ProxyAuthUser
	prefixes string
}

func (m manifest) managedProxyUsers() []string {
	if !m.ManagedProxyAuth {
		return nil
	}
	return m.proxyUsers
}

func (a *App) GetProxyAuth(ctx context.Context) (ProxyAuthSettings, error) {
	var settings ProxyAuthSettings
	err := a.locked(ctx, func() error {
		m, target, err := a.load()
		if err != nil {
			return err
		}
		settings.revision = target
		settings.SkipAuthPrefixes = m.accessRules.SkipAuthPrefixes
		for _, entry := range m.proxyUsers {
			name, _, _ := strings.Cut(entry, ":")
			settings.Usernames = append(settings.Usernames, name)
		}
		return nil
	})
	return settings, err
}

func (a *App) PrepareProxyAuth(ctx context.Context, original ProxyAuthSettings, users []ProxyAuthUser) (RuntimeSettingsPlan, error) {
	return a.PrepareProxyAuthRules(ctx, original, users, original.SkipAuthPrefixes)
}

// PrepareProxyAuthRules reviews the accounts and source-network exemptions together.
func (a *App) PrepareProxyAuthRules(ctx context.Context, original ProxyAuthSettings, users []ProxyAuthUser, skipAuthPrefixes string) (RuntimeSettingsPlan, error) {
	edit := &proxyAuthEdit{revision: original.revision, users: slices.Clone(users), prefixes: skipAuthPrefixes}
	return a.prepareRuntimeSettings(ctx, nil, RuntimeSettings{}, edit)
}

func (e *proxyAuthEdit) resolve(current []string, target string) ([]string, error) {
	if e.revision != target {
		return nil, diagnostic.New("diagnostic.settings.proxyAuthStale", "proxy settings changed while editing; reopen the editor")
	}
	passwords := make(map[string]string, len(current))
	for _, entry := range current {
		name, password, _ := strings.Cut(entry, ":")
		passwords[name] = password
	}
	records := make([]string, 0, len(e.users))
	seen := make(map[string]bool, len(e.users))
	for _, user := range e.users {
		if user.OriginalUsername != "" {
			saved, ok := passwords[user.OriginalUsername]
			if !ok {
				return nil, diagnostic.New("diagnostic.settings.proxyAuthStale", "proxy settings changed while editing; reopen the editor")
			}
			delete(passwords, user.OriginalUsername)
			if user.Password == "" {
				user.Password = saved
			}
			user.OriginalUsername = ""
		}
		if err := user.Validate(); err != nil {
			return nil, err
		}
		if seen[user.Username] {
			return nil, diagnostic.New("diagnostic.settings.proxyAuthDuplicate", "each proxy user needs a distinct username")
		}
		seen[user.Username] = true
		records = append(records, user.Username+":"+user.Password)
	}
	return records, nil
}

func (u ProxyAuthUser) Validate() error {
	if strings.TrimSpace(u.Username) == "" || strings.Contains(u.Username, ":") || !validProxyAuthField(u.Username) {
		return diagnostic.New("diagnostic.settings.proxyAuthUsername", "enter a username of up to 255 bytes without colons or control characters")
	}
	if (u.Password == "" && u.OriginalUsername == "") || !validProxyAuthField(u.Password) {
		return diagnostic.New("diagnostic.settings.proxyAuthPassword", "enter a nonempty password of up to 255 bytes without control characters")
	}
	return nil
}

// RFC 1929 encodes SOCKS5 username and password lengths in single octets.
func validProxyAuthField(s string) bool {
	return len(s) <= 255 && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}
