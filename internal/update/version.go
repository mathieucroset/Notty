// Package update checks, at most once a day, whether a newer Notty release
// is published on GitHub, remembers what it found between runs, and says
// how to upgrade for the way Notty was installed.
package update

import (
	"strconv"
	"strings"
)

// Releasable reports whether v is a clean release version:
// "vMAJOR.MINOR.PATCH" with a leading "v" optional. Dev builds ("dev", a
// commit hash, git describe output, "-dirty") and pre-releases ("-rc1")
// are not.
func Releasable(v string) bool {
	_, ok := parse(v)
	return ok
}

// Newer reports whether latest is a newer semver than current. Both are
// "vMAJOR.MINOR.PATCH" (a leading "v" optional). Anything that is not a
// clean release version (see Releasable) is not comparable: Newer returns
// false.
func Newer(latest, current string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}
	c, ok := parse(current)
	if !ok {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// parse splits a clean release version into its three numbers.
func parse(v string) ([3]uint64, bool) {
	var out [3]uint64
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != len(out) {
		return out, false
	}
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return out, false
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
