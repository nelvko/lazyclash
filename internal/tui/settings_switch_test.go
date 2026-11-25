package tui

import "testing"

func TestSettingsSwitchStateColorsAndFocus(t *testing.T) {
	for _, tc := range []struct {
		name               string
		on, enabled, hover bool
		rail, thumb        string
	}{
		{"on", true, true, false, buttonSelectedBackground, "#a8cff2"},
		{"off", false, true, false, divider, muted},
		{"hover on", true, true, true, buttonSelectedBackground, "#c5e3ff"},
		{"hover off", false, true, true, divider, "#c1c8d1"},
		{"disabled on", true, false, true, divider, disabled},
		{"disabled off", false, false, true, divider, disabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rail := themed("░░░░", tc.rail, tc.rail, false)
			thumb := themed("██", tc.thumb, tc.thumb, false)
			track := thumb + rail
			if tc.on {
				track = rail + thumb
			}
			action := overviewControlButton{selected: tc.on, enabled: tc.enabled}
			for _, focused := range []bool{false, true} {
				marker := "  "
				if focused && tc.enabled {
					marker = "› "
				}
				want := themed(marker, focus, panel, false) + track
				options := buttonOptions{Surface: panel, Focused: focused, Hovered: tc.hover, Disabled: !tc.enabled}
				if got := renderSettingsSwitch(action, options); got != want {
					t.Fatalf("focused=%t: got %q, want %q", focused, got, want)
				}
			}
		})
	}
}
