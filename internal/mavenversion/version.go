// Package mavenversion ports a8.versions.ParsedVersion / VersionParser /
// BuildTimestamp. The grammar (fastparse, NoWhitespace) is:
//
//	version = digits "." digits "." digits ("-" date "_" time "_" branch)? END
//	date    = 4-digit year, 2-digit month, 2-digit day  (exactly 8 digits)
//	time    = 2-digit hour, 2-digit minute, optional 2-digit second
//	branch  = Unicode letters/digits (Java Character.isLetterOrDigit)
//
// Only strings matching this grammar are considered versions; this is why
// maven-metadata.xml includes e.g. 10.0.1 but not 10.0 or 10.0-rc1.
package mavenversion

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

// BuildTimestamp mirrors a8.versions.BuildTimestamp.
type BuildTimestamp struct {
	Year, Month, Day, Hour, Minute int
	Second                         *int // optional
}

// BuildInfo mirrors ParsedVersion.BuildInfo.
type BuildInfo struct {
	Ts     BuildTimestamp
	Branch string
}

// ParsedVersion mirrors a8.versions.ParsedVersion.
type ParsedVersion struct {
	Major, Minor, Patch int
	Build               *BuildInfo // None when absent
}

// The branch class uses Unicode letters/digits to match Java's
// Character.isLetterOrDigit (the Scala CharsWhile(_.isLetterOrDigit)).
var versionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-(\d{4})(\d{2})(\d{2})_(\d{2})(\d{2})(\d{2})?_([\p{L}\p{Nd}]+))?$`)

// parseComponent parses a numeric version component, rejecting values that
// overflow a 32-bit signed int. Scala parses these with Int.toInt, which throws
// on overflow and causes ParsedVersion.parse to fail (excluding the version).
func parseComponent(x string) (int, bool) {
	n, err := strconv.Atoi(x)
	if err != nil || n > math.MaxInt32 {
		return 0, false
	}
	return n, true
}

// Parse parses a version string, returning ok=false when it does not match.
func Parse(s string) (ParsedVersion, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return ParsedVersion{}, false
	}
	atoi := func(x string) int { n, _ := strconv.Atoi(x); return n }
	maj, ok1 := parseComponent(m[1])
	min, ok2 := parseComponent(m[2])
	pat, ok3 := parseComponent(m[3])
	if !(ok1 && ok2 && ok3) {
		return ParsedVersion{}, false
	}
	pv := ParsedVersion{Major: maj, Minor: min, Patch: pat}
	if m[4] != "" { // build info present
		ts := BuildTimestamp{
			Year:   atoi(m[4]),
			Month:  atoi(m[5]),
			Day:    atoi(m[6]),
			Hour:   atoi(m[7]),
			Minute: atoi(m[8]),
		}
		if m[9] != "" {
			sec := atoi(m[9])
			ts.Second = &sec
		}
		pv.Build = &BuildInfo{Ts: ts, Branch: m[10]}
	}
	return pv, true
}

func (ts BuildTimestamp) String() string {
	secStr := ""
	if ts.Second != nil {
		secStr = fmt.Sprintf("%02d", *ts.Second)
	}
	return fmt.Sprintf("%d%02d%02d_%02d%02d%s", ts.Year, ts.Month, ts.Day, ts.Hour, ts.Minute, secStr)
}

func (bi BuildInfo) String() string { return bi.Ts.String() + "_" + bi.Branch }

func (pv ParsedVersion) String() string {
	s := fmt.Sprintf("%d.%d.%d", pv.Major, pv.Minor, pv.Patch)
	if pv.Build != nil {
		s += "-" + pv.Build.String()
	}
	return s
}

// cmpInt returns -1/0/1.
func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// cmpOptInt orders None < Some(x) (Scala Option ordering).
func cmpOptInt(a, b *int) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return cmpInt(*a, *b)
	}
}

func cmpTimestamp(a, b BuildTimestamp) int {
	for _, c := range []int{
		cmpInt(a.Year, b.Year), cmpInt(a.Month, b.Month), cmpInt(a.Day, b.Day),
		cmpInt(a.Hour, b.Hour), cmpInt(a.Minute, b.Minute), cmpOptInt(a.Second, b.Second),
	} {
		if c != 0 {
			return c
		}
	}
	return 0
}

// cmpOptBuild orders None < Some (Scala Option ordering), Some by timestamp.
func cmpOptBuild(a, b *BuildInfo) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return cmpTimestamp(a.Ts, b.Ts)
	}
}

// Compare orders by (major, minor, patch, Option[BuildInfo]) exactly like
// ParsedVersion.orderingByMajorMinorPathBuildTimestamp.
func Compare(a, b ParsedVersion) int {
	for _, c := range []int{cmpInt(a.Major, b.Major), cmpInt(a.Minor, b.Minor), cmpInt(a.Patch, b.Patch)} {
		if c != 0 {
			return c
		}
	}
	return cmpOptBuild(a.Build, b.Build)
}
