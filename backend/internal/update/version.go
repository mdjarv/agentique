// Package update answers two questions about this server — what am I running,
// what is published — and (from V3) performs the upgrade. See docs/upgrades.md.
package update

import (
	"regexp"
	"strconv"
	"strings"
)

// Channel classifies a build. Only a clean release tag may ever be told it is
// behind: `git describe --tags --always --dirty` yields "v0.4.1-7-gab12cd3",
// and a machine someone is actively developing on must never nag.
const (
	ChannelRelease = "release"
	ChannelDev     = "dev"
)

// releaseTag matches exactly what .github/workflows/release.yml stamps into
// main.version: the pushed tag, "vMAJOR.MINOR.PATCH". Deliberately strict —
// there is no pre-release channel (decision U8), so anything else is a dev
// build by definition.
var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// Channel reports whether a version string is a released build or a local one.
func Channel(version string) string {
	if releaseTag.MatchString(version) {
		return ChannelRelease
	}
	return ChannelDev
}

// CompareVersions orders two release tags: -1 when a < b, 0 when equal or
// either is not a release tag, +1 when a > b. Unknown compares equal on
// purpose — an unparseable version can never make us claim a machine is
// behind.
func CompareVersions(a, b string) int {
	av, aok := parseTag(a)
	bv, bok := parseTag(b)
	if !aok || !bok {
		return 0
	}
	for i := range av {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

func parseTag(v string) ([3]int, bool) {
	var out [3]int
	if !releaseTag.MatchString(v) {
		return out, false
	}
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
