package tui

import "strings"

// Totals belong to the observed core, independent of the active/closed tab and
// its search. Pause keeps the same observation on screen.
func (m Model) connectionTotals(compact bool) string {
	down, up := "--", "--"
	if snapshot := m.connectionSnapshot(); snapshot != nil {
		down, up = formatBytes(snapshot.DownloadTotal), formatBytes(snapshot.UploadTotal)
	}
	download, upload := m.tr("connections.summary_download")+": ", m.tr("connections.summary_upload")+": "
	if compact {
		download, upload = "↓ ", "↑ "
	}
	return strings.Join([]string{
		themed(download, muted, bg, false) + themed(down, accent, bg, false),
		themed(upload, muted, bg, false) + themed(up, warning, bg, false),
	}, themed("  ", text, bg, false))
}
