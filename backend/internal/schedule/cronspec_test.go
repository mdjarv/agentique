package schedule

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// --- helpers ---

func mustSpec(t *testing.T, expr string) Spec {
	t.Helper()
	s, err := ParseSpec(expr)
	if err != nil {
		t.Fatalf("ParseSpec(%q): %v", expr, err)
	}
	return s
}

func utc(y int, mo time.Month, d, hh, mm int) time.Time {
	return time.Date(y, mo, d, hh, mm, 0, 0, time.UTC)
}

func stockholm(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatalf("LoadLocation(Europe/Stockholm): %v", err)
	}
	return loc
}

func maskOf(vals ...int) uint64 {
	var m uint64
	for _, v := range vals {
		m |= 1 << uint(v)
	}
	return m
}

func maskRange(lo, hi, step int) uint64 {
	var m uint64
	for v := lo; v <= hi; v += step {
		m |= 1 << uint(v)
	}
	return m
}

// chain returns n successive activations of s starting strictly after from.
func chain(t *testing.T, s Spec, from time.Time, loc *time.Location, n int) []time.Time {
	t.Helper()
	fires := make([]time.Time, 0, n)
	cur := from
	for i := 0; i < n; i++ {
		next := s.Next(cur, loc)
		if next.IsZero() {
			t.Fatalf("Next returned zero time at step %d (from %v)", i, cur)
		}
		if !next.After(cur) {
			t.Fatalf("Next not strictly after: step %d, from %v, got %v", i, cur, next)
		}
		fires = append(fires, next)
		cur = next
	}
	return fires
}

// --- parsing ---

func TestParseSpecValid(t *testing.T) {
	allMinutes := maskRange(0, 59, 1)
	allHours := maskRange(0, 23, 1)
	allDOM := maskRange(1, 31, 1)
	allMonths := maskRange(1, 12, 1)
	allDOW := maskRange(0, 6, 1)

	cases := []struct {
		expr string
		want Spec
	}{
		{"* * * * *", Spec{allMinutes, allHours, allDOM, allMonths, allDOW, true, true}},
		{"*/5 * * * *", Spec{maskRange(0, 59, 5), allHours, allDOM, allMonths, allDOW, true, true}},
		{"0 9 * * 1-5", Spec{maskOf(0), maskOf(9), allDOM, allMonths, maskRange(1, 5, 1), true, false}},
		{"30 14 15 3 *", Spec{maskOf(30), maskOf(14), maskOf(15), maskOf(3), allDOW, false, true}},
		{"0 0 13 * 5", Spec{maskOf(0), maskOf(0), maskOf(13), allMonths, maskOf(5), false, false}},
		// "*/2" in dom counts as restricted (field text is not exactly "*").
		{"1,15,31 2-4 */2 */3 *", Spec{maskOf(1, 15, 31), maskRange(2, 4, 1), maskRange(1, 31, 2), maskRange(1, 12, 3), allDOW, false, true}},
		{"0-10/2 * * * *", Spec{maskRange(0, 10, 2), allHours, allDOM, allMonths, allDOW, true, true}},
		{"1-5/2 * * * *", Spec{maskOf(1, 3, 5), allHours, allDOM, allMonths, allDOW, true, true}},
		// dow 7 normalizes to 0 (Sunday), as a value and inside a range.
		{"59 23 31 12 7", Spec{maskOf(59), maskOf(23), maskOf(31), maskOf(12), maskOf(0), false, false}},
		{"0 0 * * 5-7", Spec{maskOf(0), maskOf(0), allDOM, allMonths, maskOf(5, 6, 0), true, false}},
		// Extra whitespace is tolerated.
		{"  5   4  3   2  1 ", Spec{maskOf(5), maskOf(4), maskOf(3), maskOf(2), maskOf(1), false, false}},
		// Impossible dates are still valid parses per vixie.
		{"0 0 29 2 *", Spec{maskOf(0), maskOf(0), maskOf(29), maskOf(2), allDOW, false, true}},
		{"0 0 31 2 *", Spec{maskOf(0), maskOf(0), maskOf(31), maskOf(2), allDOW, false, true}},
	}
	for _, tc := range cases {
		got, err := ParseSpec(tc.expr)
		if err != nil {
			t.Errorf("ParseSpec(%q): unexpected error: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSpec(%q) = %+v, want %+v", tc.expr, got, tc.want)
		}
	}

	if a, b := mustSpec(t, "0 0 * * 7"), mustSpec(t, "0 0 * * 0"); a != b {
		t.Errorf("dow 7 and dow 0 should parse identically: %+v vs %+v", a, b)
	}
}

func TestParseSpecInvalid(t *testing.T) {
	cases := []struct {
		expr    string
		wantErr string // substring that must appear in the error
	}{
		{"", "empty expression"},
		{"   ", "empty expression"},
		{"* * * *", "expected 5 fields"},
		{"* * * * * *", "expected 5 fields"},
		{"@hourly", "expected 5 fields"},
		{"60 * * * *", "out of range"},
		{"* 24 * * *", "out of range"},
		{"* * 0 * *", "out of range"},
		{"* * 32 * *", "out of range"},
		{"* * * 0 *", "out of range"},
		{"* * * 13 *", "out of range"},
		{"* * * * 8", "out of range"},
		{"0-60 * * * *", "out of range"},
		{"*/0 * * * *", "step must be at least 1"},
		{"*/-1 * * * *", "step must be at least 1"},
		{"*/x * * * *", "unsupported character"}, // rejected by the charset guard
		{"*/1-2 * * * *", "invalid step"},
		{"*/ * * * *", "invalid step"},
		{"1-5/2/3 * * * *", "invalid step"},
		{"5/2 * * * *", "step requires"},
		{"30-10 * * * *", "reversed range"},
		{"* * * * 5-1", "reversed range"},
		{"1-5-9 * * * *", "invalid range"},
		{"-5 * * * *", "invalid range"},
		{"1- * * * *", "invalid range"},
		{"*-5 * * * *", "invalid range"},
		{"1,,5 * * * *", "empty list element"},
		{",1 * * * *", "empty list element"},
		{"** * * * *", "invalid value"},
		{"* * * JAN *", "unsupported character"},
		{"* * * * MON", "unsupported character"},
		{"0 0 L * *", "unsupported character"},
		{"0 0 1W * *", "unsupported character"},
		{"* * ? * *", "unsupported character"},
	}
	for _, tc := range cases {
		_, err := ParseSpec(tc.expr)
		if err == nil {
			t.Errorf("ParseSpec(%q): expected error containing %q, got nil", tc.expr, tc.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("ParseSpec(%q) error = %q, want substring %q", tc.expr, err, tc.wantErr)
		}
	}
}

// --- Next basics in UTC ---

func TestNextBasics(t *testing.T) {
	cases := []struct {
		name string
		expr string
		from time.Time
		want time.Time
	}{
		{"every minute", "* * * * *", utc(2026, 1, 5, 10, 30), utc(2026, 1, 5, 10, 31)},
		{"every minute mid-minute seconds", "* * * * *", time.Date(2026, 1, 5, 10, 30, 30, 0, time.UTC), utc(2026, 1, 5, 10, 31)},
		{"every 5 minutes", "*/5 * * * *", utc(2026, 1, 5, 10, 31), utc(2026, 1, 5, 10, 35)},
		{"every 5 minutes on-boundary is strictly after", "*/5 * * * *", utc(2026, 1, 5, 10, 35), utc(2026, 1, 5, 10, 40)},
		{"hourly at :07", "7 * * * *", utc(2026, 1, 5, 10, 6), utc(2026, 1, 5, 10, 7)},
		{"hourly at :07 wraps", "7 * * * *", utc(2026, 1, 5, 10, 7), utc(2026, 1, 5, 11, 7)},
		{"hour wrap to next hour", "0 * * * *", utc(2026, 1, 5, 10, 59), utc(2026, 1, 5, 11, 0)},
		{"daily 09:00 before", "0 9 * * *", utc(2026, 1, 5, 8, 59), utc(2026, 1, 5, 9, 0)},
		{"daily 09:00 on-boundary", "0 9 * * *", utc(2026, 1, 5, 9, 0), utc(2026, 1, 6, 9, 0)},
		{"weekdays 09:00 monday morning", "0 9 * * 1-5", utc(2026, 1, 5, 8, 0), utc(2026, 1, 5, 9, 0)},
		{"weekdays 09:00 skips weekend", "0 9 * * 1-5", utc(2026, 1, 9, 10, 0), utc(2026, 1, 12, 9, 0)}, // Fri 10:00 -> Mon 09:00
		{"monthly", "30 14 15 3 *", utc(2026, 1, 5, 0, 0), utc(2026, 3, 15, 14, 30)},
		{"monthly wraps a year", "30 14 15 3 *", utc(2026, 3, 15, 14, 30), utc(2027, 3, 15, 14, 30)},
		{"comma list mid-day", "0 8,12,17 * * *", utc(2026, 1, 5, 9, 0), utc(2026, 1, 5, 12, 0)},
		{"comma list wraps day", "0 8,12,17 * * *", utc(2026, 1, 5, 17, 0), utc(2026, 1, 6, 8, 0)},
		{"year wrap", "0 0 1 1 *", utc(2026, 3, 1, 0, 0), utc(2027, 1, 1, 0, 0)},
		{"dom 31 skips short months", "0 0 31 * *", utc(2026, 4, 1, 0, 0), utc(2026, 5, 31, 0, 0)},
		{"last minute of year", "59 23 31 12 *", utc(2026, 1, 1, 0, 0), utc(2026, 12, 31, 23, 59)},
		{"feb 29 waits for leap year", "0 0 29 2 *", utc(2026, 1, 1, 0, 0), utc(2028, 2, 29, 0, 0)},
	}
	for _, tc := range cases {
		got := mustSpec(t, tc.expr).Next(tc.from, time.UTC)
		if !got.Equal(tc.want) {
			t.Errorf("%s: Next(%q, %v) = %v, want %v", tc.name, tc.expr, tc.from, got, tc.want)
		}
	}
}

func TestNextVixieDomDow(t *testing.T) {
	cases := []struct {
		name string
		expr string
		from time.Time
		want time.Time
	}{
		// Both restricted: 13th OR Friday. 2026-02-06 and 2026-02-13 are Fridays.
		{"friday before the 13th", "0 0 13 * 5", utc(2026, 2, 1, 0, 0), utc(2026, 2, 6, 0, 0)},
		{"friday the 13th", "0 0 13 * 5", utc(2026, 2, 6, 0, 0), utc(2026, 2, 13, 0, 0)},
		{"friday after the 13th", "0 0 13 * 5", utc(2026, 2, 13, 0, 0), utc(2026, 2, 20, 0, 0)},
		// 2026-04-13 is a Monday: the dom side fires without a dow match...
		{"13th that is not friday", "0 0 13 * 5", utc(2026, 4, 11, 0, 0), utc(2026, 4, 13, 0, 0)},
		// ...and the dow side keeps firing between 13ths.
		{"next friday after monday the 13th", "0 0 13 * 5", utc(2026, 4, 13, 0, 0), utc(2026, 4, 17, 0, 0)},
		// Only dom restricted: dow is ignored.
		{"only dom", "0 0 15 * *", utc(2026, 1, 20, 0, 0), utc(2026, 2, 15, 0, 0)},
		// Only dow restricted: dom is ignored. 2026-01-11 is a Sunday.
		{"only dow", "0 0 * * 0", utc(2026, 1, 5, 0, 0), utc(2026, 1, 11, 0, 0)},
		{"only dow with 7 as sunday", "0 0 * * 7", utc(2026, 1, 5, 0, 0), utc(2026, 1, 11, 0, 0)},
		// "*/2" in dom is restricted, so OR applies: odd days OR Mondays.
		{"step dom OR dow, dom side", "0 0 */2 * 1", utc(2026, 1, 1, 0, 30), utc(2026, 1, 3, 0, 0)},
		{"step dom OR dow, dow side", "0 0 */2 * 1", utc(2026, 1, 11, 10, 0), utc(2026, 1, 12, 0, 0)}, // Mon Jan 12 (even dom)
		// dom impossible for the month, but the dow side still fires (OR).
		{"impossible dom rescued by dow", "0 0 31 2 5", utc(2026, 1, 1, 0, 0), utc(2026, 2, 6, 0, 0)},
	}
	for _, tc := range cases {
		got := mustSpec(t, tc.expr).Next(tc.from, time.UTC)
		if !got.Equal(tc.want) {
			t.Errorf("%s: Next(%q, %v) = %v, want %v", tc.name, tc.expr, tc.from, got, tc.want)
		}
	}
}

// --- DST vectors, hand-derived from tzdata for Europe/Stockholm ---
//
// Spring forward 2026-03-29: 02:00 CET -> 03:00 CEST at 01:00 UTC
// (wall 02:00-02:59 does not exist).
// Fall back 2026-10-25: 03:00 CEST -> 02:00 CET at 01:00 UTC
// (wall 02:00-02:59 occurs twice: CEST = 00:00-00:59 UTC, CET = 01:00-01:59 UTC).

func TestNextSpringForwardDaily(t *testing.T) {
	loc := stockholm(t)
	s := mustSpec(t, "30 2 * * *")

	// Normal day before the transition: 02:30 CET = 01:30 UTC.
	if got, want := s.Next(utc(2026, 3, 27, 12, 0), loc), utc(2026, 3, 28, 1, 30); !got.Equal(want) {
		t.Errorf("day before transition: got %v, want %v", got.UTC(), want)
	}
	// Transition day: wall 02:30 does not exist; the fire normalizes forward
	// to 03:30 CEST = 01:30 UTC.
	if got, want := s.Next(utc(2026, 3, 28, 12, 0), loc), utc(2026, 3, 29, 1, 30); !got.Equal(want) {
		t.Errorf("transition day normalized-forward fire: got %v, want %v", got.UTC(), want)
	}
	// From just before the jump (01:59 CET) the normalized fire is still
	// strictly ahead.
	if got, want := s.Next(utc(2026, 3, 29, 0, 59), loc), utc(2026, 3, 29, 1, 30); !got.Equal(want) {
		t.Errorf("from inside pre-jump hour: got %v, want %v", got.UTC(), want)
	}
	// Next day is back to 02:30 CEST = 00:30 UTC.
	if got, want := s.Next(utc(2026, 3, 29, 1, 30), loc), utc(2026, 3, 30, 0, 30); !got.Equal(want) {
		t.Errorf("day after transition: got %v, want %v", got.UTC(), want)
	}
}

func TestNextSpringForwardEveryMinute(t *testing.T) {
	loc := stockholm(t)
	s := mustSpec(t, "* * * * *")

	// Wall clock jumps 01:59 CET -> 03:00 CEST with no gap in real time: the
	// nonexistent 02:xx combos normalize onto 03:xx and fire once.
	fires := chain(t, s, utc(2026, 3, 29, 0, 58), loc, 4)
	want := []time.Time{
		utc(2026, 3, 29, 0, 59), // 01:59 CET
		utc(2026, 3, 29, 1, 0),  // 03:00 CEST (wall 02:00 normalized forward)
		utc(2026, 3, 29, 1, 1),
		utc(2026, 3, 29, 1, 2),
	}
	for i := range want {
		if !fires[i].Equal(want[i]) {
			t.Errorf("fire %d: got %v, want %v", i, fires[i].UTC(), want[i])
		}
	}
	if hh, mm, _ := fires[1].In(loc).Clock(); hh != 3 || mm != 0 {
		t.Errorf("fire after the jump should read 03:00 wall, got %02d:%02d", hh, mm)
	}
}

func TestNextFallBackHourly(t *testing.T) {
	loc := stockholm(t)
	s := mustSpec(t, "0 * * * *")

	// Wall hours 00:00..06:00 on 2026-10-25. The repeated wall hour (02:xx)
	// fires once, at its first (CEST) pass: 02:00 CEST = 00:00 UTC. The next
	// fire is 03:00 CET = 02:00 UTC — a 2h real-time gap.
	fires := chain(t, s, utc(2026, 10, 24, 21, 30), loc, 7)
	want := []time.Time{
		utc(2026, 10, 24, 22, 0), // 00:00 CEST
		utc(2026, 10, 24, 23, 0), // 01:00 CEST
		utc(2026, 10, 25, 0, 0),  // 02:00 CEST (first pass of the repeated hour)
		utc(2026, 10, 25, 2, 0),  // 03:00 CET  (2h UTC gap)
		utc(2026, 10, 25, 3, 0),  // 04:00 CET
		utc(2026, 10, 25, 4, 0),  // 05:00 CET
		utc(2026, 10, 25, 5, 0),  // 06:00 CET
	}
	for i := range want {
		if !fires[i].Equal(want[i]) {
			t.Errorf("fire %d: got %v, want %v", i, fires[i].UTC(), want[i])
		}
	}
	// Exactly one 2h gap, all other gaps 1h; each wall-clock hour fires once.
	twoHourGaps := 0
	for i := 1; i < len(fires); i++ {
		switch gap := fires[i].Sub(fires[i-1]); gap {
		case time.Hour:
		case 2 * time.Hour:
			twoHourGaps++
		default:
			t.Errorf("unexpected gap %v between fires %d and %d", gap, i-1, i)
		}
	}
	if twoHourGaps != 1 {
		t.Errorf("expected exactly one 2h gap, got %d", twoHourGaps)
	}
	for i, f := range fires {
		if hh, mm, _ := f.In(loc).Clock(); hh != i || mm != 0 {
			t.Errorf("fire %d wall clock = %02d:%02d, want %02d:00", i, hh, mm, i)
		}
	}
}

func TestNextFallBackHalfHourly(t *testing.T) {
	loc := stockholm(t)
	s := mustSpec(t, "*/30 * * * *")

	// Repeated wall combos 02:00/02:30 fire once each (first pass), so the
	// step from 02:30 CEST (00:30 UTC) to 03:00 CET (02:00 UTC) is a 90m gap.
	fires := chain(t, s, utc(2026, 10, 24, 22, 30), loc, 8)
	want := []time.Time{
		utc(2026, 10, 24, 23, 0),  // 01:00 CEST
		utc(2026, 10, 24, 23, 30), // 01:30 CEST
		utc(2026, 10, 25, 0, 0),   // 02:00 CEST
		utc(2026, 10, 25, 0, 30),  // 02:30 CEST
		utc(2026, 10, 25, 2, 0),   // 03:00 CET (90m gap)
		utc(2026, 10, 25, 2, 30),  // 03:30 CET
		utc(2026, 10, 25, 3, 0),   // 04:00 CET
		utc(2026, 10, 25, 3, 30),  // 04:30 CET
	}
	for i := range want {
		if !fires[i].Equal(want[i]) {
			t.Errorf("fire %d: got %v, want %v", i, fires[i].UTC(), want[i])
		}
	}
	ninetyMinuteGaps := 0
	for i := 1; i < len(fires); i++ {
		switch gap := fires[i].Sub(fires[i-1]); gap {
		case 30 * time.Minute:
		case 90 * time.Minute:
			ninetyMinuteGaps++
		default:
			t.Errorf("unexpected gap %v between fires %d and %d", gap, i-1, i)
		}
	}
	if ninetyMinuteGaps != 1 {
		t.Errorf("expected exactly one 90m gap, got %d", ninetyMinuteGaps)
	}
}

func TestNextFallBackDaily(t *testing.T) {
	loc := stockholm(t)
	s := mustSpec(t, "30 2 * * *")

	// Wall 02:30 occurs twice on 2026-10-25; the schedule fires exactly once
	// for that calendar date, at the first occurrence (02:30 CEST = 00:30 UTC).
	fires := chain(t, s, utc(2026, 10, 23, 12, 0), loc, 4)
	want := []time.Time{
		utc(2026, 10, 24, 0, 30), // 02:30 CEST
		utc(2026, 10, 25, 0, 30), // 02:30 CEST, first pass of the repeated hour
		utc(2026, 10, 26, 1, 30), // 02:30 CET
		utc(2026, 10, 27, 1, 30), // 02:30 CET
	}
	for i := range want {
		if !fires[i].Equal(want[i]) {
			t.Errorf("fire %d: got %v, want %v", i, fires[i].UTC(), want[i])
		}
	}
	onTransitionDate := 0
	for _, f := range fires {
		if y, mo, d := f.In(loc).Date(); y == 2026 && mo == time.October && d == 25 {
			onTransitionDate++
		}
	}
	if onTransitionDate != 1 {
		t.Errorf("expected exactly one fire on 2026-10-25, got %d", onTransitionDate)
	}

	// From just after the first occurrence, Next must NOT return the second
	// occurrence (02:30 CET = 01:30 UTC on the 25th): wall-clock semantics
	// say the next fire is on the 26th.
	from := utc(2026, 10, 25, 0, 31)
	got := s.Next(from, loc)
	secondOccurrence := utc(2026, 10, 25, 1, 30)
	if got.Equal(secondOccurrence) {
		t.Fatalf("Next returned the second occurrence of the repeated wall time: %v", got.UTC())
	}
	if wantNext := utc(2026, 10, 26, 1, 30); !got.Equal(wantNext) {
		t.Errorf("Next(%v) = %v, want %v", from, got.UTC(), wantNext)
	}
}

func TestNextFallBackEveryMinute(t *testing.T) {
	loc := stockholm(t)
	s := mustSpec(t, "* * * * *")

	// Each wall combo fires once: after 02:59 CEST (00:59 UTC) the next wall
	// minute is 03:00 CET (02:00 UTC) — the repeated 02:xx CET pass is
	// skipped, leaving a 61-minute real-time gap.
	fires := chain(t, s, utc(2026, 10, 25, 0, 57), loc, 4)
	want := []time.Time{
		utc(2026, 10, 25, 0, 58), // 02:58 CEST
		utc(2026, 10, 25, 0, 59), // 02:59 CEST
		utc(2026, 10, 25, 2, 0),  // 03:00 CET
		utc(2026, 10, 25, 2, 1),  // 03:01 CET
	}
	for i := range want {
		if !fires[i].Equal(want[i]) {
			t.Errorf("fire %d: got %v, want %v", i, fires[i].UTC(), want[i])
		}
	}
}

func TestNextFallBackStrictlyFutureGuard(t *testing.T) {
	loc := stockholm(t)

	// t inside the repeated hour's second pass (02:10 CET = 01:10 UTC). The
	// wall candidate 02:30 canonicalizes to its first pass (00:30 UTC) which
	// is <= t; the guard must skip it, never returning a first-pass instant.
	s := mustSpec(t, "*/30 * * * *")
	from := utc(2026, 10, 25, 1, 10)
	got := s.Next(from, loc)
	if !got.After(from) {
		t.Fatalf("Next(%v) = %v, not strictly after", from, got.UTC())
	}
	if firstPass := utc(2026, 10, 25, 0, 30); got.Equal(firstPass) {
		t.Fatalf("Next returned the first-pass (CEST) instant %v from inside the second pass", got.UTC())
	}
	if want := utc(2026, 10, 25, 2, 0); !got.Equal(want) { // 03:00 CET
		t.Errorf("Next(%v) = %v, want %v", from, got.UTC(), want)
	}

	// Same for an hourly spec from 02:30 CET.
	s = mustSpec(t, "0 * * * *")
	from = utc(2026, 10, 25, 1, 30)
	got = s.Next(from, loc)
	if !got.After(from) {
		t.Fatalf("Next(%v) = %v, not strictly after", from, got.UTC())
	}
	if want := utc(2026, 10, 25, 2, 0); !got.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", from, got.UTC(), want)
	}
}

// --- properties ---

func TestNextStrictlyFutureProperty(t *testing.T) {
	specs := []string{
		"* * * * *",
		"*/5 * * * *",
		"*/30 * * * *",
		"7 * * * *",
		"0 9 * * *",
		"0 9 * * 1-5",
		"30 14 15 3 *",
		"0 8,12,17 * * *",
		"0 0 13 * 5",
		"59 23 31 12 *",
		"0 0 29 2 *",
		"30 2 * * *",
	}
	locs := []*time.Location{time.UTC, stockholm(t)}

	rng := rand.New(rand.NewSource(42)) // deterministic
	base := utc(2020, 1, 1, 0, 0)
	spanSeconds := int64(10 * 365 * 24 * 3600)

	for _, expr := range specs {
		s := mustSpec(t, expr)
		for i := 0; i < 200; i++ {
			at := base.Add(time.Duration(rng.Int63n(spanSeconds)) * time.Second)
			for _, loc := range locs {
				n1 := s.Next(at, loc)
				if n1.IsZero() {
					t.Fatalf("%q: Next(%v, %v) returned zero time", expr, at, loc)
				}
				if !n1.After(at) {
					t.Fatalf("%q: Next(%v, %v) = %v is not strictly after", expr, at, loc, n1.UTC())
				}
				n2 := s.Next(n1, loc)
				if n2.IsZero() {
					t.Fatalf("%q: Next(Next) from %v in %v returned zero time", expr, n1.UTC(), loc)
				}
				if !n2.After(n1) {
					t.Fatalf("%q: Next(Next(%v)) = %v is not strictly after %v in %v", expr, at, n2.UTC(), n1.UTC(), loc)
				}
			}
		}
	}
}

func TestNextImpossibleDate(t *testing.T) {
	locs := []*time.Location{time.UTC, stockholm(t)}
	for _, expr := range []string{"0 0 31 2 *", "0 0 30 2 *", "0 0 31 4 *"} {
		s := mustSpec(t, expr) // valid parse per vixie, just never matches
		for _, loc := range locs {
			if got := s.Next(utc(2026, 1, 5, 10, 0), loc); !got.IsZero() {
				t.Errorf("%q: Next in %v = %v, want zero time", expr, loc, got.UTC())
			}
		}
	}
}

func TestNextNilLocationDefaultsUTC(t *testing.T) {
	s := mustSpec(t, "0 9 * * *")
	got := s.Next(utc(2026, 1, 5, 8, 0), nil)
	if want := utc(2026, 1, 5, 9, 0); !got.Equal(want) {
		t.Errorf("Next with nil loc = %v, want %v", got.UTC(), want)
	}
}
