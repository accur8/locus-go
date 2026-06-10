package mavenversion

import (
	"sort"
	"testing"
)

func TestParseAcceptReject(t *testing.T) {
	accept := []string{"10.0.1", "11.0.2", "16.0.1", "1.0.0-20230408_0712_master", "1.0.0-20221106_1442_glenneodeploy", "2.7.0-20180418_0536_master"}
	reject := []string{"10.0", "19.0", "10.0-rc1", "2.0.0-RC6", "", "abc", "1.0", "1.0.0-rc1"}
	for _, s := range accept {
		if _, ok := Parse(s); !ok {
			t.Errorf("expected parse OK: %q", s)
		}
	}
	for _, s := range reject {
		if _, ok := Parse(s); ok {
			t.Errorf("expected parse FAIL: %q", s)
		}
	}
}

func TestBuildInfoParse(t *testing.T) {
	pv, ok := Parse("1.0.0-20230408_0712_master")
	if !ok {
		t.Fatal("parse failed")
	}
	if pv.Major != 1 || pv.Minor != 0 || pv.Patch != 0 {
		t.Errorf("mmp = %d.%d.%d", pv.Major, pv.Minor, pv.Patch)
	}
	if pv.Build == nil {
		t.Fatal("no build info")
	}
	ts := pv.Build.Ts
	if ts.Year != 2023 || ts.Month != 4 || ts.Day != 8 || ts.Hour != 7 || ts.Minute != 12 || ts.Second != nil {
		t.Errorf("ts = %+v", ts)
	}
	if pv.Build.Branch != "master" {
		t.Errorf("branch = %q", pv.Build.Branch)
	}
	if pv.String() != "1.0.0-20230408_0712_master" {
		t.Errorf("String = %q", pv.String())
	}
}

func TestOrdering(t *testing.T) {
	in := []string{
		"1.0.0-20221202_1611_master",
		"1.0.0-20221106_1442_glenneodeploy",
		"1.0.0-20230408_0712_master",
		"1.0.0-20221202_1617_master",
	}
	var pvs []ParsedVersion
	for _, s := range in {
		pv, _ := Parse(s)
		pvs = append(pvs, pv)
	}
	sort.Slice(pvs, func(i, j int) bool { return Compare(pvs[i], pvs[j]) < 0 })
	want := "1.0.0-20230408_0712_master"
	if got := pvs[len(pvs)-1].String(); got != want {
		t.Errorf("max = %q want %q", got, want)
	}
	if got := pvs[0].String(); got != "1.0.0-20221106_1442_glenneodeploy" {
		t.Errorf("min = %q", got)
	}
}

func TestNumericOverflowRejected(t *testing.T) {
	// > 2^31-1 components overflow Scala's Int and are excluded; Go must match.
	for _, s := range []string{"99999999999.0.0", "1.0.99999999999999999999"} {
		if _, ok := Parse(s); ok {
			t.Errorf("expected overflow component to be rejected: %q", s)
		}
	}
}

func TestUnicodeBranchAccepted(t *testing.T) {
	// Java isLetterOrDigit accepts Unicode letters in the branch segment.
	if _, ok := Parse("1.0.0-20230408_0712_naïve"); !ok {
		t.Errorf("expected unicode branch to parse")
	}
}

func TestNoneBeforeSome(t *testing.T) {
	plain, _ := Parse("1.0.0")
	built, _ := Parse("1.0.0-20230408_0712_master")
	if Compare(plain, built) >= 0 {
		t.Errorf("plain 1.0.0 should sort before build-stamped")
	}
}
