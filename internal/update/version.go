// Package update finds out whether a newer basa has been released, and
// replaces the running binary with it.
//
// It reads the same GitHub release the install script does, the same way:
// /releases/latest resolved by its redirect, then basa-<os>-<arch> verified
// against checksums.txt before anything is made executable. The asset names
// here, in scripts/install.sh, and in .github/workflows/release.yml have to stay
// in step.
//
// Nothing here carries a Basa token. The only destination is the public
// release page, and the only thing it learns is this build's version, from the
// User-Agent.
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a released version: exactly major.minor.patch.
//
// Anything else is not a release. A `make build` stamps git describe output
// such as v0.1.6-3-gabc1234-dirty, and a plain `go build` stamps "dev". Neither
// can be ordered against a release honestly — the first is ahead of v0.1.6 and
// behind nothing we know of — so they parse as not-a-release, and the caller
// decides what that means.
type Version struct {
	Major, Minor, Patch int
}

// Parse reads "0.1.7" or "v0.1.7". The release workflow stamps the first, the
// tag is the second, and both mean the same release.
func Parse(s string) (Version, bool) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return Version{}, false
	}

	var nums [3]int
	for i, p := range parts {
		// Atoi alone accepts "+1" and "-1"; a version component is digits only.
		if p == "" || strings.TrimLeft(p, "0123456789") != "" {
			return Version{}, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, false
		}
		nums[i] = n
	}

	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, true
}

// Less reports whether v is older than o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// String is the tag form, which is what the releases page shows.
func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}
