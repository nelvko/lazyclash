package mihomo

import (
	"strings"
	"testing"
)

const v2CPUFlags = "pni cx16 lahf_lm popcnt ssse3 sse4_1 sse4_2"
const v3CPUFlags = v2CPUFlags + " avx avx2 bmi1 bmi2 f16c fma abm movbe xsave"

func cpuInfo(flags ...string) []byte {
	var result strings.Builder
	for _, set := range flags {
		result.WriteString("processor : 0\nflags : " + set + "\n\n")
	}
	return []byte(result.String())
}

func TestAMD64LevelRequiresEveryInstructionAndOSAVXSupport(t *testing.T) {
	for _, test := range []struct {
		name  string
		flags []string
		avxOS bool
		want  int
	}{
		{"v3", []string{v3CPUFlags}, true, 3},
		{"v2", []string{v2CPUFlags}, true, 2},
		{"baseline", []string{"sse2"}, true, 1},
		{"kernel-without-ymm-state", []string{v3CPUFlags}, false, 2},
		{"weakest-v2-core", []string{v3CPUFlags, v2CPUFlags}, true, 2},
		{"weakest-v1-core", []string{v3CPUFlags, "sse2"}, true, 1},
		{"no-flags", []string{""}, true, 1},
		{"unknown", nil, true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := amd64Level(cpuInfo(test.flags...), test.avxOS); got != test.want {
				t.Fatalf("level=%d, want %d", got, test.want)
			}
		})
	}
	for _, flag := range strings.Fields(v3CPUFlags) {
		t.Run("missing-"+flag, func(t *testing.T) {
			flags := strings.Fields(v3CPUFlags)
			var remaining []string
			for _, candidate := range flags {
				if candidate != flag {
					remaining = append(remaining, candidate)
				}
			}
			want := 2
			if strings.Contains(" "+v2CPUFlags+" ", " "+flag+" ") {
				want = 1
			}
			if got := amd64Level(cpuInfo(v3CPUFlags, strings.Join(remaining, " ")), true); got != want {
				t.Fatalf("level=%d missing %s on second core, want %d", got, flag, want)
			}
		})
	}
	if got := amd64Level(append(cpuInfo(v3CPUFlags), []byte("processor : 1\nmodel name : modern CPU\n")...), true); got != 1 {
		t.Fatalf("incomplete CPU data used model inference: %d", got)
	}
	aliases := strings.ReplaceAll(strings.ReplaceAll(v3CPUFlags, "pni", "sse3"), "abm", "lzcnt")
	if got := amd64Level(cpuInfo(aliases), true); got != 3 {
		t.Fatalf("instruction aliases gave level %d", got)
	}
}
