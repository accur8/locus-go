package javatime

import (
	"testing"
	"time"
)

func TestStringOmitsZeroSeconds(t *testing.T) {
	d := FromYMDHMS(2023, time.April, 8, 7, 12, 0)
	if got := d.String(); got != "2023-04-08T07:12" {
		t.Errorf("String = %q want 2023-04-08T07:12", got)
	}
	d2 := FromYMDHMS(2023, time.April, 8, 7, 13, 1)
	if got := d2.String(); got != "2023-04-08T07:13:01" {
		t.Errorf("String = %q want 2023-04-08T07:13:01", got)
	}
}

func TestMavenLastUpdatedReplicatesHourBug(t *testing.T) {
	// build time 07:12 -> production emits 20230408121200 (minute twice).
	d := FromYMDHMS(2023, time.April, 8, 7, 12, 0)
	if got := d.MavenLastUpdated(); got != "20230408121200" {
		t.Errorf("MavenLastUpdated = %q want 20230408121200", got)
	}
}

func TestUberParse(t *testing.T) {
	d, err := UberParse("2015-12-09 20:58")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.String(); got != "2015-12-09T20:58" {
		t.Errorf("String = %q", got)
	}
}
