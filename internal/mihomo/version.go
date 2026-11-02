package mihomo

import "strings"

type Channel string

const (
	ChannelStable  Channel = "stable"
	ChannelAlpha   Channel = "alpha"
	ChannelUnknown Channel = "unknown"
)

type UpdateStatus string

const (
	UpdateCurrent      UpdateStatus = "current"
	UpdateAvailable    UpdateStatus = "available"
	UpdateAhead        UpdateStatus = "ahead"
	UpdateIncomparable UpdateStatus = "incomparable"
	UpdateNotInstalled UpdateStatus = "not-installed"
	UpdateUnavailable  UpdateStatus = "unavailable"
)

// validVersionToken is shared by version.txt and executable output. Unknown
// version-shaped tokens remain readable without being assumed to be ordered.
func validVersionToken(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, c := range value {
		if !asciiAlphaNumeric(c) && c != '.' && c != '-' && c != '_' && c != '+' {
			return false
		}
	}
	return decimalDigit(value[0]) || (len(value) > 1 && value[0] == 'v' && decimalDigit(value[1])) ||
		(strings.HasPrefix(value, "alpha-") && len(value) > len("alpha-"))
}

func asciiAlphaNumeric(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func decimalDigit(c byte) bool { return c >= '0' && c <= '9' }

func ChannelForVersion(version string) Channel {
	if !validVersionToken(version) {
		return ChannelUnknown
	}
	if strings.HasPrefix(version, "alpha-") {
		hash := strings.TrimPrefix(version, "alpha-")
		if len(hash) >= 7 && len(hash) <= 40 {
			valid := true
			for _, c := range hash {
				valid = valid && (c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
			}
			if valid {
				return ChannelAlpha
			}
		}
		return ChannelUnknown
	}
	if _, ok := parseSemanticVersion(version); ok {
		return ChannelStable
	}
	return ChannelUnknown
}

// CompareVersions orders only semantic stable versions. Alpha hashes identify
// builds but carry no chronological ordering; equal identifiers are all that
// can be established without querying commit history.
func CompareVersions(installed, latest string, channel Channel) UpdateStatus {
	if installed == "" {
		return UpdateNotInstalled
	}
	if !validVersionToken(installed) || !validVersionToken(latest) {
		return UpdateIncomparable
	}
	if channel == ChannelAlpha {
		if ChannelForVersion(installed) == ChannelAlpha && installed == latest {
			return UpdateCurrent
		}
		return UpdateIncomparable
	}
	if channel != ChannelStable {
		return UpdateIncomparable
	}
	local, localOK := parseSemanticVersion(installed)
	remote, remoteOK := parseSemanticVersion(latest)
	if !localOK || !remoteOK {
		return UpdateIncomparable
	}
	switch compareSemanticVersions(local, remote) {
	case -1:
		return UpdateAvailable
	case 1:
		return UpdateAhead
	default:
		return UpdateCurrent
	}
}

type semanticVersion struct {
	core [3]string
	pre  []string
}

func parseSemanticVersion(value string) (semanticVersion, bool) {
	var version semanticVersion
	if !validVersionToken(value) {
		return version, false
	}
	value = strings.TrimPrefix(value, "v")
	if main, build, ok := strings.Cut(value, "+"); ok {
		if !validIdentifiers(build, false) {
			return version, false
		}
		value = main
	}
	if core, pre, ok := strings.Cut(value, "-"); ok {
		if !validIdentifiers(pre, true) {
			return version, false
		}
		version.pre = strings.Split(pre, ".")
		value = core
	}
	core := strings.Split(value, ".")
	if len(core) != 3 {
		return version, false
	}
	for i, number := range core {
		if !numericIdentifier(number) || len(number) > 1 && number[0] == '0' {
			return version, false
		}
		version.core[i] = number
	}
	return version, true
}

func validIdentifiers(value string, prerelease bool) bool {
	for _, part := range strings.Split(value, ".") {
		if part == "" || prerelease && numericIdentifier(part) && len(part) > 1 && part[0] == '0' {
			return false
		}
		for _, c := range part {
			if !asciiAlphaNumeric(c) && c != '-' {
				return false
			}
		}
	}
	return true
}

func numericIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i := range value {
		if !decimalDigit(value[i]) {
			return false
		}
	}
	return true
}

func compareNumber(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func compareSemanticVersions(left, right semanticVersion) int {
	for i := range left.core {
		if difference := compareNumber(left.core[i], right.core[i]); difference != 0 {
			return difference
		}
	}
	if len(left.pre) == 0 && len(right.pre) != 0 {
		return 1
	}
	if len(left.pre) != 0 && len(right.pre) == 0 {
		return -1
	}
	for i := 0; i < len(left.pre) && i < len(right.pre); i++ {
		a, b := left.pre[i], right.pre[i]
		var difference int
		switch {
		case numericIdentifier(a) && numericIdentifier(b):
			difference = compareNumber(a, b)
		case numericIdentifier(a):
			difference = -1
		case numericIdentifier(b):
			difference = 1
		default:
			difference = strings.Compare(a, b)
		}
		if difference != 0 {
			return difference
		}
	}
	if len(left.pre) < len(right.pre) {
		return -1
	}
	if len(left.pre) > len(right.pre) {
		return 1
	}
	return 0
}
