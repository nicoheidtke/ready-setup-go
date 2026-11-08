package installer

import (
	"fmt"
	"regexp"
	"strconv"
)

type Version struct {
	Minor int
	Patch int
}

func (v Version) String() string {
	return fmt.Sprintf("1.%d.%d", v.Minor, v.Patch)
}

func (v Version) Less(other Version) bool {
	return v.Minor < other.Minor || v.Minor == other.Minor && v.Patch < other.Patch
}

type Selector struct {
	Version
	Exact bool
}

var versionPattern = regexp.MustCompile(`^1\.([0-9]+)(?:\.([0-9]+|x))?$`)

func ParseSelector(s string) (Selector, error) {
	return parseSelector(s, 26)
}

func parseSelector(s string, minimumMinor int) (Selector, error) {
	match := versionPattern.FindStringSubmatch(s)
	if match == nil {
		return Selector{}, fmt.Errorf("invalid Go version %q: use 1.26 or newer, e.g. 1.26.1 or 1.26.x", s)
	}
	minor, err := strconv.Atoi(match[1])
	if err != nil || minor < minimumMinor || strconv.Itoa(minor) != match[1] {
		return Selector{}, fmt.Errorf("unsupported Go version %q", s)
	}
	selector := Selector{Version: Version{Minor: minor}}
	if match[2] != "" && match[2] != "x" {
		patch, err := strconv.Atoi(match[2])
		if err != nil || strconv.Itoa(patch) != match[2] {
			return Selector{}, fmt.Errorf("invalid patch in %q", s)
		}
		selector.Patch, selector.Exact = patch, true
	}
	return selector, nil
}

func (s Selector) Matches(v Version) bool {
	return s.Minor == v.Minor && (!s.Exact || s.Patch == v.Patch)
}
