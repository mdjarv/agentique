// Package schedule implements the in-house cron subset used by scheduled
// loops (docs/scheduled-loops.md): 5-field vixie-style expressions supporting
// wildcards, single values, ranges, steps, and comma lists. Deliberately
// rejected: L/W/?, name aliases (MON/JAN), a seconds field, and @macros.
package schedule

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Spec is a parsed 5-field cron expression (minute hour day-of-month month
// day-of-week). The zero value matches nothing; obtain a Spec via ParseSpec.
type Spec struct {
	minute uint64 // bits 0-59
	hour   uint64 // bits 0-23
	dom    uint64 // bits 1-31
	month  uint64 // bits 1-12
	dow    uint64 // bits 0-6; 7 (Sunday) is normalized to 0 at parse time

	// Vixie day semantics: a day field counts as restricted when its text is
	// not exactly "*" (so "*/2" is restricted). When both are restricted, a
	// date matches if EITHER matches; when only one is, that one must match.
	domStar bool
	dowStar bool
}

// fieldDef describes one cron field's name, value bounds, and normalization.
type fieldDef struct {
	name string
	min  int
	max  int
	mod  int // normalization modulus applied to each value; 0 = none
}

var (
	fieldMinute = fieldDef{name: "minute", min: 0, max: 59}
	fieldHour   = fieldDef{name: "hour", min: 0, max: 23}
	fieldDOM    = fieldDef{name: "day-of-month", min: 1, max: 31}
	fieldMonth  = fieldDef{name: "month", min: 1, max: 12}
	fieldDOW    = fieldDef{name: "day-of-week", min: 0, max: 7, mod: 7} // 7 == Sunday == 0
)

// ParseSpec parses a 5-field cron expression (minute hour dom month dow).
// Supported per field: "*", single values, ranges "a-b", steps "*/n" and
// "a-b/n", and comma lists of those. Field ranges: minute 0-59, hour 0-23,
// day-of-month 1-31, month 1-12, day-of-week 0-7 (0 and 7 are both Sunday).
func ParseSpec(expr string) (Spec, error) {
	fields := strings.Fields(expr)
	if len(fields) == 0 {
		return Spec{}, errors.New("cron: empty expression")
	}
	if len(fields) != 5 {
		return Spec{}, fmt.Errorf("cron: expected 5 fields (minute hour day-of-month month day-of-week), got %d in %q", len(fields), expr)
	}

	var s Spec
	defs := []struct {
		def  fieldDef
		mask *uint64
	}{
		{fieldMinute, &s.minute},
		{fieldHour, &s.hour},
		{fieldDOM, &s.dom},
		{fieldMonth, &s.month},
		{fieldDOW, &s.dow},
	}
	for i, d := range defs {
		mask, err := parseField(fields[i], d.def)
		if err != nil {
			return Spec{}, fmt.Errorf("cron: parse %q: %w", expr, err)
		}
		*d.mask = mask
	}
	s.domStar = fields[2] == "*"
	s.dowStar = fields[4] == "*"
	return s, nil
}

// parseField parses one field (a comma list of terms) into a bitmask.
func parseField(text string, def fieldDef) (uint64, error) {
	if err := checkCharset(text, def); err != nil {
		return 0, err
	}
	var mask uint64
	for _, term := range strings.Split(text, ",") {
		m, err := parseTerm(term, def)
		if err != nil {
			return 0, err
		}
		mask |= m
	}
	return mask, nil
}

// checkCharset rejects any character outside the restricted grammar up front
// so L, W, ?, and name aliases fail with a targeted message rather than a
// generic number-parse error.
func checkCharset(text string, def fieldDef) error {
	for _, r := range text {
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '*', ',', '-', '/':
			continue
		}
		return fmt.Errorf("%s field %q: unsupported character %q (names, L, W, and ? are not supported)", def.name, text, r)
	}
	return nil
}

// parseTerm parses a single list element: "*", "v", "a-b", "*/n", or "a-b/n".
func parseTerm(term string, def fieldDef) (uint64, error) {
	if term == "" {
		return 0, fmt.Errorf("%s field: empty list element", def.name)
	}

	body, stepText, hasStep := strings.Cut(term, "/")
	step := 1
	if hasStep {
		n, err := strconv.Atoi(stepText)
		if err != nil {
			return 0, fmt.Errorf("%s field: invalid step %q in %q", def.name, stepText, term)
		}
		if n < 1 {
			return 0, fmt.Errorf("%s field: step must be at least 1, got %d in %q", def.name, n, term)
		}
		step = n
	}

	var lo, hi int
	switch {
	case body == "*":
		lo, hi = def.min, def.max
	case strings.Contains(body, "-"):
		loText, hiText, _ := strings.Cut(body, "-")
		a, aErr := strconv.Atoi(loText)
		b, bErr := strconv.Atoi(hiText)
		if aErr != nil || bErr != nil {
			return 0, fmt.Errorf("%s field: invalid range %q (want a-b)", def.name, body)
		}
		if err := checkBounds(a, def); err != nil {
			return 0, err
		}
		if err := checkBounds(b, def); err != nil {
			return 0, err
		}
		if a > b {
			return 0, fmt.Errorf("%s field: reversed range %q (%d > %d)", def.name, body, a, b)
		}
		lo, hi = a, b
	default:
		v, err := strconv.Atoi(body)
		if err != nil {
			return 0, fmt.Errorf("%s field: invalid value %q", def.name, body)
		}
		if hasStep {
			return 0, fmt.Errorf("%s field: step requires \"*\" or a range, got %q", def.name, term)
		}
		if err := checkBounds(v, def); err != nil {
			return 0, err
		}
		return bitFor(v, def), nil
	}

	var mask uint64
	for v := lo; v <= hi; v += step {
		mask |= bitFor(v, def)
	}
	return mask, nil
}

func checkBounds(v int, def fieldDef) error {
	if v < def.min || v > def.max {
		return fmt.Errorf("%s field: value %d out of range %d-%d", def.name, v, def.min, def.max)
	}
	return nil
}

func bitFor(v int, def fieldDef) uint64 {
	if def.mod > 0 {
		v %= def.mod
	}
	return 1 << uint(v)
}

func bitSet(mask uint64, v int) bool {
	return mask&(1<<uint(v)) != 0
}

// searchYears bounds Next's calendar walk. Five years covers every
// satisfiable spec reachable from present-day starts (the widest real gap is
// Feb 29, at most four years apart until 2100); only impossible dates like
// Feb 31 exhaust the bound.
const searchYears = 5

// Next returns the first activation strictly after t, evaluated as a
// wall-clock calendar search in loc (nil defaults to time.UTC). It never
// returns an instant <= t. If no activation exists within searchYears — an
// impossible date such as "0 0 31 2 *", which is still a valid parse per
// vixie — it returns the zero time.
//
// DST semantics, chosen deliberately (docs/scheduled-loops.md):
//   - Spring-forward: a nonexistent wall time (e.g. 02:30 when clocks jump
//     02:00->03:00) is accepted as time.Date's forward-normalized instant
//     (02:30 -> 03:30) — the fire shifts forward rather than skipping the
//     day. This is the accept-normalized-forward choice for hour/minute
//     specs; the candidate is not re-validated against the spec fields.
//   - Fall-back: the search is over wall-clock combinations, so a wall time
//     in the repeated hour fires once, at its earliest UTC instant. Go's own
//     resolution of ambiguous wall times is unspecified (observed: the later
//     pass), so ambiguous candidates are canonicalized to the first
//     occurrence via earliestForWall. An hourly spec therefore has a 2h
//     real-time gap once a year; a */30 spec a 90m gap.
//   - Regardless of the above, any candidate whose UTC instant is <= t is
//     discarded and the search continues — the strictly-future guard is
//     unconditional.
func (s Spec) Next(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}

	wall := t.In(loc)
	y, mo, d := wall.Date()
	hh, mm, _ := wall.Clock()
	c := cursor{y: y, mo: mo, d: d, hh: hh, mm: mm}
	// Start one wall-clock minute past t's minute: minute granularity plus
	// the strictly-after contract.
	c.nextMinute()

	limit := c.y + searchYears
	for c.y <= limit {
		switch {
		case !bitSet(s.month, int(c.mo)):
			c.nextMonth()
		case !s.dayMatches(c.y, c.mo, c.d):
			c.nextDay()
		case !bitSet(s.hour, c.hh):
			c.nextHour()
		case !bitSet(s.minute, c.mm):
			c.nextMinute()
		default:
			cand := earliestForWall(time.Date(c.y, c.mo, c.d, c.hh, c.mm, 0, 0, loc))
			if cand.After(t) {
				return cand
			}
			// Ambiguous fall-back wall time resolved to an instant at or
			// before t (t sits in the repeated hour's second pass) — keep
			// walking forward in wall-clock space.
			c.nextMinute()
		}
	}
	return time.Time{}
}

// dayMatches applies the vixie day rule for the calendar date (y, mo, d).
func (s Spec) dayMatches(y int, mo time.Month, d int) bool {
	domHit := bitSet(s.dom, d)
	// A date's weekday is a pure calendar fact; compute it in UTC to stay
	// clear of loc's DST edges.
	dowHit := bitSet(s.dow, int(time.Date(y, mo, d, 0, 0, 0, 0, time.UTC).Weekday()))
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dowHit
	case s.dowStar:
		return domHit
	default:
		// Both restricted: vixie OR — either field may match.
		return domHit || dowHit
	}
}

// cursor is a wall-clock calendar position advanced field-by-field. Each
// advance resets all lesser fields to their minimum, so the search always
// lands on the earliest wall time within the advanced unit.
type cursor struct {
	y      int
	mo     time.Month
	d      int
	hh, mm int
}

func (c *cursor) nextMinute() {
	c.mm++
	if c.mm > 59 {
		c.nextHour()
	}
}

func (c *cursor) nextHour() {
	c.mm = 0
	c.hh++
	if c.hh > 23 {
		c.nextDay()
	}
}

func (c *cursor) nextDay() {
	c.mm, c.hh = 0, 0
	c.d++
	if c.d > daysIn(c.y, c.mo) {
		c.nextMonth()
	}
}

func (c *cursor) nextMonth() {
	c.mm, c.hh, c.d = 0, 0, 1
	c.mo++
	if c.mo > 12 {
		c.mo = 1
		c.y++
	}
}

// daysIn returns the number of days in (y, mo). Day 0 of the following month
// normalizes to the last day of mo.
func daysIn(y int, mo time.Month) int {
	return time.Date(y, mo+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// earliestForWall canonicalizes a fall-back-ambiguous wall time to its first
// (earliest) UTC instant so behavior is deterministic — Go's time.Date
// resolution of ambiguous wall times is unspecified. It probes the common
// DST deltas (1h, 30m); an unambiguous time is returned unchanged.
func earliestForWall(c time.Time) time.Time {
	for _, delta := range []time.Duration{time.Hour, 30 * time.Minute} {
		alt := c.Add(-delta)
		if sameWall(alt, c) {
			return alt
		}
	}
	return c
}

// sameWall reports whether a and b show the same wall-clock date and
// hour:minute in their (shared) location.
func sameWall(a, b time.Time) bool {
	ay, amo, ad := a.Date()
	by, bmo, bd := b.Date()
	ah, am, _ := a.Clock()
	bh, bm, _ := b.Clock()
	return ay == by && amo == bmo && ad == bd && ah == bh && am == bm
}
