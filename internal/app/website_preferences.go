package app

import (
	"context"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

type Website struct {
	Name string
	URL  string
}

func DefaultWebsites() []Website {
	return []Website{
		{"GitHub", "https://github.com/"},
		{"Google", "https://www.google.com/"},
		{"YouTube", "https://www.youtube.com/"},
	}
}

func (p Preferences) WebsitesForTesting() []Website {
	if p.Websites == nil {
		return DefaultWebsites()
	}
	return slices.Clone(p.Websites)
}

func (s Website) normalized() (Website, error) {
	raw := strings.TrimSpace(s.URL)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, "?#") || strings.HasSuffix(u.Host, ":") {
		return Website{}, invalidWebsiteURL()
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	port := u.Port()
	bracketed := strings.HasPrefix(u.Host, "[")
	u.Host = host
	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.String()
		u.Host = host
		if ip.Is6() {
			u.Host = "[" + host + "]"
		}
	} else if bracketed || !validWebsiteHost(host) {
		return Website{}, invalidWebsiteURL()
	}
	if port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return Website{}, invalidWebsiteURL()
		}
		if !(u.Scheme == "https" && n == 443 || u.Scheme == "http" && n == 80) {
			u.Host += ":" + strconv.FormatUint(n, 10)
		}
	}
	if u.Path == "" {
		u.Path = "/"
	}
	if !utf8.ValidString(s.Name) || strings.IndexFunc(s.Name, unicode.IsControl) >= 0 {
		return Website{}, diagnostic.New("diagnostic.website.name", "Enter a website name without control characters")
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		s.Name = host
	}
	s.URL = u.String()
	return s, nil
}

func invalidWebsiteURL() error {
	return diagnostic.New("diagnostic.website.url", "Enter a hostname or HTTP(S) URL without credentials, query or fragment")
}

func validWebsiteHost(host string) bool {
	for _, label := range strings.Split(host, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-') {
				return false
			}
		}
	}
	return true
}

func uniqueWebsite(sites []Website, site Website) error {
	for _, saved := range sites {
		if strings.EqualFold(saved.Name, site.Name) || saved.URL == site.URL {
			return diagnostic.New("diagnostic.website.duplicate", "A website with this name or address already exists")
		}
	}
	return nil
}

func (a *App) AddWebsite(ctx context.Context, site Website) ([]Website, error) {
	site, err := site.normalized()
	if err != nil {
		return nil, err
	}
	var updated []Website
	err = a.editPreferences(ctx, func(p *Preferences) error {
		sites := p.WebsitesForTesting()
		if err := uniqueWebsite(sites, site); err != nil {
			return err
		}
		updated = append(sites, site)
		p.Websites = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (a *App) RemoveWebsite(ctx context.Context, site Website) ([]Website, error) {
	var updated, currentOnConflict []Website
	err := a.editPreferences(ctx, func(p *Preferences) error {
		sites := p.WebsitesForTesting()
		i := slices.Index(sites, site)
		if i < 0 {
			currentOnConflict = sites
			return diagnostic.New("diagnostic.website.missing", "This website has changed or was removed")
		}
		updated = slices.Delete(sites, i, i+1)
		p.Websites = updated
		return nil
	})
	if err != nil {
		return currentOnConflict, err
	}
	return updated, nil
}
