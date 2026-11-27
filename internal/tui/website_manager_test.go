package tui

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestWebsiteManagerMovesSelectedSiteAfterSave(t *testing.T) {
	sites := app.DefaultWebsites()
	b := &fakeBackend{snapshot: settingsTestSnapshot(), moveWebsite: func(_ context.Context, site app.Website, direction int) ([]app.Website, error) {
		if site != sites[0] || direction != 1 {
			t.Fatalf("unexpected move: %v, %d", site, direction)
		}
		return []app.Website{sites[1], site, sites[2]}, nil
	}}
	m := ready(b)
	directionCleanup(t, &m)
	m.activate("website.manage")
	m.layout()
	if m.form != "website-manager" || m.siteManagerAction("websiteManager.up") != nil || hasAction(m.targets, "websiteManager.up") {
		t.Fatal("the first website allowed moving up")
	}
	cmd := m.siteManagerKey(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	if cmd == nil || !m.busy || !slices.Equal(m.websites, sites) || m.siteManagerAction("websiteManager.down") != nil {
		t.Fatal("move was lost, applied early, or submitted twice")
	}
	result := cmd().(websiteMoveMsg)
	m.acceptWebsiteMove(result)
	if m.busy || !slices.Equal(m.websites, result.sites) || m.siteManager.selected != 1 || m.focus != "websiteManager.list" {
		t.Fatalf("selection did not follow the saved order: %v, %d, %s", m.websites, m.siteManager.selected, m.focus)
	}
	b.moveWebsite = func(context.Context, app.Website, int) ([]app.Website, error) {
		return nil, errors.New("save failed")
	}
	cmd = m.siteManagerAction("websiteManager.up")
	m.acceptWebsiteMove(cmd().(websiteMoveMsg))
	if m.siteManager.err == "" || m.siteManager.selected != 1 || !slices.Equal(m.websites, result.sites) {
		t.Fatal("save failure changed the order or hid the error")
	}
}

func TestWebsiteResultsFollowURLAcrossEditsAndReordering(t *testing.T) {
	sites := app.DefaultWebsites()
	m := Model{websites: sites}
	result := app.WebsiteTestResult{StatusCode: 200, Duration: 42 * time.Millisecond}
	m.websiteTests.rows = map[string]websiteTestRow{sites[0].URL: {result: result}}
	moved := []app.Website{sites[1], {Name: "New name", URL: sites[0].URL}, sites[2]}
	m.setWebsites(moved)
	if m.websiteTests.rows[moved[1].URL].result != result {
		t.Fatal("renaming or moving a site lost the result for its URL")
	}
	changed := slices.Clone(moved)
	changed[1].URL = "https://example.org/"
	m.setWebsites(changed)
	if len(m.websiteTests.rows) != 0 {
		t.Fatal("editing the URL retained the old result")
	}
}
