package mihomo

import (
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/cpu"
)

// RecommendedVariant names the native build selected before release metadata is
// fetched. A release that lacks this build may use a lower compatible level.
func RecommendedVariant() string {
	if runtime.GOARCH == "amd64" {
		return "amd64-v" + strconv.Itoa(nativeAMD64Level())
	}
	return runtime.GOARCH
}

func nativeAMD64Level() int {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return 1
	}
	raw, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 1
	}
	// AVX also needs kernel support for saving XMM/YMM state. x/sys/cpu
	// checks both CPUID and XGETBV, unlike a CPU model or /proc flag alone.
	avxOS := cpu.X86.HasOSXSAVE && cpu.X86.HasAVX && cpu.X86.HasAVX2
	return amd64Level(raw, avxOS)
}

// amd64Level uses the weakest visible CPU. A guest therefore uses the features
// exposed by its hypervisor, without inferring capabilities from a model name.
func amd64Level(cpuinfo []byte, avxOS bool) int {
	level, processors, featureSets := 3, 0, 0
	for line := range strings.SplitSeq(string(cpuinfo), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "processor":
			processors++
		case "flags":
			featureSets++
			flags := make(map[string]bool)
			for _, flag := range strings.Fields(value) {
				flags[flag] = true
			}
			has := func(required ...string) bool {
				for _, flag := range required {
					if !flags[flag] {
						return false
					}
				}
				return true
			}
			// Linux calls SSE3 "pni" and LZCNT "abm" on x86.
			v2 := (flags["pni"] || flags["sse3"]) && has("cx16", "lahf_lm", "popcnt", "ssse3", "sse4_1", "sse4_2")
			if !v2 {
				level = 1
			} else if !avxOS || !(flags["abm"] || flags["lzcnt"]) || !has("avx", "avx2", "bmi1", "bmi2", "f16c", "fma", "movbe", "xsave") {
				level = min(level, 2)
			}
		}
	}
	if processors == 0 || featureSets != processors {
		return 1
	}
	return level
}
