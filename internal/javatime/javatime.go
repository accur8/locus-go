// Package javatime mirrors a8.locus.model.DateTime, reproducing Java
// LocalDateTime.toString formatting and the maven-metadata <lastUpdated>
// formatting (including the original code's hour==getMinute bug, which the
// production server exhibits and which we replicate for byte-for-byte parity).
//
// All wall-clock fields are interpreted in the process-local time zone, exactly
// like the JVM (ZoneId.systemDefault()). For exact parity with production set
// TZ to the same zone the JVM ran in.
package javatime

import (
	"fmt"
	"time"
)

// DateTime wraps an instant, exposing wall-clock fields in the local zone.
type DateTime struct {
	t time.Time // stored in local zone
}

// FromEpochMillis builds a DateTime from epoch milliseconds (S3 lastModified).
func FromEpochMillis(ms int64) DateTime {
	return DateTime{t: time.UnixMilli(ms).In(time.Local)}
}

// FromYMDHMS builds a DateTime from explicit local wall-clock components.
func FromYMDHMS(year int, month time.Month, day, hour, minute, second int) DateTime {
	return DateTime{t: time.Date(year, month, day, hour, minute, second, 0, time.Local)}
}

// Empty mirrors DateTime.empty == apply(0L): epoch 0 in the local zone.
func Empty() DateTime { return FromEpochMillis(0) }

var uberLayout = "2006-01-02 15:04"

// UberParse parses "yyyy-MM-dd HH:mm" in the local zone (ReadMavenIndexDotHtml).
func UberParse(s string) (DateTime, error) {
	t, err := time.ParseInLocation(uberLayout, s, time.Local)
	if err != nil {
		return DateTime{}, err
	}
	return DateTime{t: t}, nil
}

// Epoch mirrors Scala DateTime.epoc = value.toEpochSecond(ZoneOffset.UTC):
// the wall-clock fields reinterpreted at the UTC offset. Used only for ordering.
func (d DateTime) Epoch() int64 {
	return time.Date(d.t.Year(), d.t.Month(), d.t.Day(), d.t.Hour(), d.t.Minute(), d.t.Second(), 0, time.UTC).Unix()
}

// String reproduces Java LocalDateTime.toString: "yyyy-MM-ddTHH:mm" and appends
// ":ss" only when seconds are non-zero (second-precision; we never have nanos).
func (d DateTime) String() string {
	base := d.t.Format("2006-01-02T15:04")
	if d.t.Second() != 0 {
		base += fmt.Sprintf(":%02d", d.t.Second())
	}
	return base
}

// MavenLastUpdated reproduces GenerateMavenMetadata's lastUpdated string:
//
//	f"${year}%04d${month.getValue}%02d${day}%02d${hour}%02d${minute}%02d${second}%02d"
//
// where DateTime.hour is (faithfully) value.getMinute — i.e. the minute is
// emitted twice. This reproduces the production output (e.g. build time 07:12 ->
// 20230408121200, and DateTime.empty -> 19691231000000 in a UTC-behind zone).
func (d DateTime) MavenLastUpdated() string {
	minute := d.t.Minute()
	return fmt.Sprintf("%04d%02d%02d%02d%02d%02d",
		d.t.Year(), int(d.t.Month()), d.t.Day(), minute, minute, d.t.Second())
}
