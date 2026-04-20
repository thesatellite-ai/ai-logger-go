package importer

import (
	"strconv"
	"strings"
)

// versionCmp returns -1, 0, +1 like strings.Compare but understands
// dot-separated numeric segments with optional dash-prerelease suffixes
// (semver-ish).
//
// Comparison order:
//
//	1. Strip prerelease for the primary compare ("0.118.0-alpha.2" → "0.118.0").
//	2. Walk numeric segments left-to-right; missing segments compare as 0.
//	3. If primary segments tie AND both sides had a prerelease, compare
//	   prereleases lexicographically.
//	4. A version with NO prerelease beats one WITH a prerelease at the
//	   same primary triple ("0.118.0" > "0.118.0-alpha.2"), matching
//	   semver convention.
//
// Empty strings sort lowest. Non-numeric segments are treated as zero,
// which is a conservative choice — if a tool ever ships "x.y.z-foo"
// vs "x.y.z-bar" we'd compare on the suffix string instead.
func versionCmp(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}

	aPrim, aPre := splitPrerelease(a)
	bPrim, bPre := splitPrerelease(b)

	if c := compareNumericSegments(aPrim, bPrim); c != 0 {
		return c
	}
	// Same primary triple — semver says "no prerelease > has prerelease".
	if aPre == "" && bPre != "" {
		return 1
	}
	if aPre != "" && bPre == "" {
		return -1
	}
	return strings.Compare(aPre, bPre)
}

// splitPrerelease returns ("0.118.0", "alpha.2") for "0.118.0-alpha.2".
func splitPrerelease(v string) (primary, pre string) {
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

// compareNumericSegments walks dot-separated numeric segments. Non-
// numeric segments are coerced to 0 so unexpected chunks don't crash
// the comparison.
func compareNumericSegments(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(as) {
			ai, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bi, _ = strconv.Atoi(bs[i])
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}
