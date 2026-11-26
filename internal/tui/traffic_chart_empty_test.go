package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestTrafficChartEmptyLabelFollowsTrafficObservations(t *testing.T) {
	now := trafficChartTestNow()
	zero := func(age time.Duration) trafficPoint { return trafficPoint{At: now.Add(-age), Valid: true} }
	failed := trafficPoint{At: now.Add(-time.Second)}
	peak := zero(time.Second)
	peak.Down = 512
	for _, tc := range []struct {
		name    string
		history []trafficPoint
		adjust  func(*dashboardOptions)
		want    string
	}{
		{"first status pending", nil, func(o *dashboardOptions) { o.RuntimeReceived, o.Runtime.State = false, "" }, "等待采样"},
		{"ready without a traffic frame", nil, nil, "等待采样"},
		{"zero first frame", []trafficPoint{zero(0)}, nil, "暂无流量"},
		{"fresh at three seconds", []trafficPoint{zero(6 * time.Second), zero(3 * time.Second)}, nil, "暂无流量"},
		{"expired peak before window", []trafficPoint{{At: now.Add(-time.Minute - time.Second), Down: 30, Valid: true}, zero(0)}, nil, "暂无流量"},
		{"stale zero while other sources refresh", []trafficPoint{zero(3*time.Second + 1)}, nil, ""},
		{"gap in otherwise idle history", []trafficPoint{zero(3*time.Second + 1), zero(0)}, nil, ""},
		{"invalid history then valid zero", []trafficPoint{failed, zero(0)}, nil, ""},
		{"previous curve with idle latest frame", []trafficPoint{peak, zero(0)}, nil, ""},
		{"traffic source unavailable", nil, func(o *dashboardOptions) { o.Runtime.TrafficError = "stream error" }, ""},
		{"error snapshot with zero history", []trafficPoint{zero(0)}, func(o *dashboardOptions) { o.Runtime.State = "unavailable" }, ""},
		{"core stopped before first frame", nil, func(o *dashboardOptions) { o.Snapshot.Service.Active = false }, ""},
		{"API unavailable before first runtime update", nil, func(o *dashboardOptions) { o.RuntimeReceived, o.Snapshot.APIError = false, "unavailable" }, ""},
		{"core uninstalled", nil, func(o *dashboardOptions) { o.Snapshot.Installed = false }, ""},
		{"unrelated source error", []trafficPoint{zero(0)}, func(o *dashboardOptions) { o.Runtime.MemoryError = "stream error" }, "暂无流量"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := dashboardOptions{
				Locale: i18n.New("zh-Hans"), TrafficHistory: tc.history, TrafficNow: now,
				Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true}},
				Runtime:  app.RuntimeSnapshot{State: "ready", ObservedAt: now}, RuntimeReceived: true,
			}
			if len(tc.history) != 0 {
				o.Runtime.Traffic = &mihomo.Traffic{}
			}
			if tc.adjust != nil {
				tc.adjust(&o)
			}
			plot := ansi.Strip(strings.Join(overviewTrafficChart(o, 40, 4), "\n"))
			for _, label := range []string{"等待采样", "暂无流量"} {
				if strings.Contains(plot, label) != (label == tc.want) {
					t.Fatalf("want %q in chart:\n%s", tc.want, plot)
				}
			}
		})
	}
}

func TestTrafficChartEmptyLabelPreservesGridAndData(t *testing.T) {
	now := trafficChartTestNow()
	for _, language := range []string{"en", "zh-Hans"} {
		o := dashboardOptions{
			Locale: i18n.New(language), TrafficNow: now, RuntimePending: true,
			Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true}},
		}
		label := o.Locale.T("dashboard.overview.trafficWaiting")
		for _, size := range [][2]int{{128, 6}, {40, 4}, {8, 4}, {1, 1}} {
			g := newTrafficChartGeometry(size[0], size[1], makeTrafficChartScale(0), now)
			cells := make([]trafficChartCell, g.columns*g.height)
			rows := trafficChartRows(o, g, cells)
			plain := func(row int) string { return ansi.Strip(rows[row]) }
			for _, row := range rows {
				if ansi.StringWidth(row) != size[0] {
					t.Fatalf("%s/%v empty label changed row width", language, size)
				}
			}
			if size[1] < 4 || ansi.StringWidth(label) > g.columns {
				if strings.Contains(strings.Join(rows, "\n"), label) {
					t.Fatalf("%s/%v empty label must fit in full", language, size)
				}
				continue
			}
			center := g.rateY(g.scale.maximum/2)/4 - 1
			left, _, found := strings.Cut(plain(center), label)
			if !found || ansi.StringWidth(left) != g.axis+(g.columns-ansi.StringWidth(label))/2 {
				t.Fatalf("%s/%v empty label misplaced: %q", language, size, plain(center))
			}
			withoutLabel := o
			withoutLabel.Snapshot.Installed = false
			want := trafficChartRows(withoutLabel, g, cells)
			rows[center] = want[center]
			if !slices.Equal(want, rows) {
				t.Fatalf("%s/%v grid or time axis changed", language, size)
			}
			cells[0].down = 1
			if strings.Contains(strings.Join(trafficChartRows(o, g, cells), "\n"), label) {
				t.Fatal("an empty label was shown over a visible curve")
			}
		}
	}
}
