package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func overviewWebsiteCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	inner := overviewInnerWidth(width)
	available := o.websiteTestsAvailable()
	slot := func(site, label, detail, action string) dashboardBlock {
		id := "website." + site
		button := dashboardButtonOptions(id, o, false, panel)
		button.Kind = buttonText
		button.Busy = !o.websiteTests.enabled(site)
		// Keep the focused action visible while its request is pending.
		button.Disabled = !available || button.Busy && o.Focus != id
		if button.Disabled {
			id = ""
		}
		face := renderButton(action, button)
		if detail != "" && ansi.StringWidth(label)+ansi.StringWidth(detail)+ansi.StringWidth(face)+4 <= inner {
			label, detail = label+"  "+detail, ""
		}
		block := overviewLabelSlot(o, inner, label, face, id, true)
		if detail != "" {
			block.append(overviewTextBlock([]string{detail}, inner))
		}
		return block
	}
	title := slot("all", overviewHeading(o, tr("dashboard.website.title")), "", tr("dashboard.website.all"))
	var content dashboardBlock
	sites := app.WebsiteTestSites()
	nameWidth := 0
	for _, site := range sites {
		nameWidth = max(nameWidth, ansi.StringWidth(site))
	}
	for i, site := range sites {
		if i > 0 {
			content.blank()
		}
		label := cellPad(overviewHeading(o, site), nameWidth)
		content.append(slot(site, label, o.websiteTestStatus(site), tr("dashboard.website.test")))
	}
	return overviewPanel(o, width, title, content, nil)
}

func (o dashboardOptions) websiteTestsAvailable() bool {
	return !o.StateUnavailable && !o.Busy && overviewProxyAvailable(o.Snapshot)
}

func (o dashboardOptions) websiteTestStatus(site string) string {
	tr := o.Locale.T
	row := o.websiteTests.rows[site]
	color, status := muted, tr("dashboard.website.untested")
	code := row.result.StatusCode
	switch {
	case o.StateUnavailable || !overviewProxyAvailable(o.Snapshot):
		return themed(tr("dashboard.status.unavailable"), muted, panel, o.Dim)
	case row.pending():
		return themed(tr("dashboard.website.running"), accent, panel, o.Dim)
	case errors.Is(row.err, context.DeadlineExceeded):
		color, status = warning, tr("dashboard.website.timeout")
	case row.err != nil:
		color, status = errorColor, tr("dashboard.website.failed")
	case code >= 200 && code < 300:
		color, status = success, tr("dashboard.website.reachable")
	case code == http.StatusForbidden:
		color, status = warning, tr("dashboard.website.forbidden")
	case code != 0:
		color, status = warning, fmt.Sprintf("HTTP %d", code)
	}
	value := themed(status, color, panel, o.Dim)
	if code != 0 && row.err == nil {
		value += themed(fmt.Sprintf(" · %d ms", row.result.Duration.Milliseconds()), muted, panel, o.Dim)
	}
	if age := profileAge(row.checkedAt, o.Now, o.Locale); age != "" {
		value += themed(" · "+age, muted, panel, o.Dim)
	}
	return value
}
