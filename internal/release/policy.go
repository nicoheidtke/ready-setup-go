package release

import "strings"

// releaseAliases are mutable convenience tags; full versions remain fixed.
func releaseAliases(tag string) []string {
	parts := strings.Split(tag, ".")
	return []string{"refs/tags/" + parts[0], "refs/tags/" + parts[0] + "." + parts[1]}
}

func versionLess(a, b string) bool {
	x, y := strings.Split(a[1:], "."), strings.Split(b[1:], ".")
	for i := range x {
		if len(x[i]) != len(y[i]) {
			return len(x[i]) < len(y[i])
		}
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

// publicationPolicy freezes latest and eligible aliases independently. A newer
// minor blocks only its major alias, not maintenance of an older minor series.
func publicationPolicy(c Config, refs map[string]string) (map[string]string, bool) {
	eligible := map[string]bool{}
	if c.UpdateAliases {
		for _, ref := range releaseAliases(c.Tag) {
			eligible[ref] = true
		}
	}
	latest := true
	for ref := range refs {
		tag := strings.TrimPrefix(ref, "refs/tags/")
		if !tagPattern.MatchString(tag) || !versionLess(c.Tag, tag) {
			continue
		}
		latest = false
		for _, alias := range releaseAliases(tag) {
			delete(eligible, alias)
		}
	}
	if c.MakeLatest == "true" {
		latest = true
	}
	if c.MakeLatest == "false" {
		latest = false
	}
	aliases := map[string]string{}
	for ref := range eligible {
		aliases[ref] = refs[ref]
	}
	return aliases, latest
}

// PreviousVersion keeps maintenance release notes in the selected version line.
func PreviousVersion(tag string, refs map[string]string) string {
	bestMinor, bestMajor := "", ""
	series := releaseAliases(tag)
	for ref := range refs {
		version := strings.TrimPrefix(ref, "refs/tags/")
		if !tagPattern.MatchString(version) || !versionLess(version, tag) {
			continue
		}
		other := releaseAliases(version)
		if series[0] == other[0] && (bestMajor == "" || versionLess(bestMajor, version)) {
			bestMajor = version
		}
		if series[1] == other[1] && (bestMinor == "" || versionLess(bestMinor, version)) {
			bestMinor = version
		}
	}
	if bestMinor != "" {
		return bestMinor
	}
	return bestMajor
}
