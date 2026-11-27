package app

import (
	"context"
	"errors"
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

// Normalized returns the name and URL used to identify the saved website.
func (s Website) Normalized() (Website, error) {
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
	site, err := site.Normalized()
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

func (a *App) UpdateWebsite(ctx context.Context, original, replacement Website) ([]Website, error) {
	return a.editWebsiteAt(ctx, original, func(sites []Website, i int) ([]Website, error) {
		site, err := replacement.Normalized()
		if err != nil {
			return nil, err
		}
		others := slices.Delete(sites, i, i+1)
		if err := uniqueWebsite(others, site); err != nil {
			return nil, err
		}
		return slices.Insert(others, i, site), nil
	})
}

func (a *App) RemoveWebsite(ctx context.Context, site Website) ([]Website, error) {
	return a.editWebsiteAt(ctx, site, func(sites []Website, i int) ([]Website, error) {
		return slices.Delete(sites, i, i+1), nil
	})
}

// MoveWebsite swaps the site with its current neighbor in the locked list.
func (a *App) MoveWebsite(ctx context.Context, original Website, direction int) ([]Website, error) {
	if direction != -1 && direction != 1 {
		return nil, errors.New("website move direction must be -1 or 1")
	}
	return a.editWebsiteAt(ctx, original, func(sites []Website, i int) ([]Website, error) {
		j := i + direction
		if j >= 0 && j < len(sites) {
			sites[i], sites[j] = sites[j], sites[i]
		}
		return sites, nil
	})
}

// RelocateWebsite resolves both ends of a drag against the same saved list.
func (a *App) RelocateWebsite(ctx context.Context, site, neighbor Website, after bool) ([]Website, error) {
	return a.editWebsiteAt(ctx, site, func(sites []Website, from int) ([]Website, error) {
		to := slices.Index(sites, neighbor)
		if to < 0 {
			return nil, errWebsiteMissing
		}
		if to == from {
			return sites, nil
		}
		others := slices.Delete(sites, from, from+1)
		if from < to {
			to--
		}
		if after {
			to++
		}
		return slices.Insert(others, to, site), nil
	})
}

var errWebsiteMissing = diagnostic.New("diagnostic.website.missing", "This website has changed or was removed")

func (a *App) editWebsiteAt(ctx context.Context, original Website, edit func([]Website, int) ([]Website, error)) ([]Website, error) {
	var current, updated []Website
	err := a.editPreferences(ctx, func(p *Preferences) error {
		current = p.WebsitesForTesting()
		i := slices.Index(current, original)
		if i < 0 {
			return errWebsiteMissing
		}
		var err error
		updated, err = edit(current, i)
		if err != nil {
			return err
		}
		p.Websites = updated
		return nil
	})
	if err == errWebsiteMissing {
		return current, err
	}
	if err != nil {
		return nil, err
	}
	return updated, nil
}
