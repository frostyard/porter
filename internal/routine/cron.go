package routine

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a routine's five cron fields: minute, hour, day of month,
// month and day of week, matched in a zone (specs/routines.md). Each field
// is *, a number, a range a-b, a step */n or a-b/n, or a list of those;
// months and weekdays may also be named (jan, mon). Sunday is 0 or 7. As in
// Vixie cron, when both day fields are restricted a day matches either.
type Schedule struct {
	minute, hour, dom, month, dow uint64
	domStar, dowStar              bool
}

var (
	monthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
	dayNames   = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
)

// ParseSchedule parses five cron fields.
func ParseSchedule(s string) (Schedule, error) {
	f := strings.Fields(s)
	if len(f) != 5 {
		return Schedule{}, fmt.Errorf("schedule %q has %d fields, not 5 (minute hour day-of-month month day-of-week)", s, len(f))
	}
	var sc Schedule
	var err error
	if sc.minute, _, err = field(f[0], 0, 59, nil); err != nil {
		return Schedule{}, fmt.Errorf("schedule's minute: %w", err)
	}
	if sc.hour, _, err = field(f[1], 0, 23, nil); err != nil {
		return Schedule{}, fmt.Errorf("schedule's hour: %w", err)
	}
	if sc.dom, sc.domStar, err = field(f[2], 1, 31, nil); err != nil {
		return Schedule{}, fmt.Errorf("schedule's day of month: %w", err)
	}
	if sc.month, _, err = field(f[3], 1, 12, monthNames); err != nil {
		return Schedule{}, fmt.Errorf("schedule's month: %w", err)
	}
	if sc.dow, sc.dowStar, err = field(f[4], 0, 7, dayNames); err != nil {
		return Schedule{}, fmt.Errorf("schedule's day of week: %w", err)
	}
	if sc.dow&(1<<7) != 0 {
		sc.dow |= 1 // 7 is Sunday too
	}
	return sc, nil
}

// field parses one field into a bit set, and says whether it is *.
func field(s string, lo, hi int, names []string) (uint64, bool, error) {
	var bits uint64
	for part := range strings.SplitSeq(s, ",") {
		rng, stepText, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return 0, false, fmt.Errorf("%q has a bad step", part)
			}
			step = n
		}
		var a, b int
		switch {
		case rng == "*":
			a, b = lo, hi
		case strings.Contains(rng, "-"):
			x, y, _ := strings.Cut(rng, "-")
			var err error
			if a, err = value(x, lo, hi, names); err != nil {
				return 0, false, err
			}
			if b, err = value(y, lo, hi, names); err != nil {
				return 0, false, err
			}
			if a > b {
				return 0, false, fmt.Errorf("%q runs backwards", rng)
			}
		default:
			v, err := value(rng, lo, hi, names)
			if err != nil {
				return 0, false, err
			}
			a, b = v, v
			if hasStep {
				b = hi // a/n means from a to the end, every n
			}
		}
		for v := a; v <= b; v += step {
			bits |= 1 << v
		}
	}
	return bits, s == "*", nil
}

func value(s string, lo, hi int, names []string) (int, error) {
	for i, n := range names {
		if strings.EqualFold(s, n) {
			return i + lo, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if v < lo || v > hi {
		return 0, fmt.Errorf("%d is outside %d-%d", v, lo, hi)
	}
	return v, nil
}

// Matches says whether t's minute, in t's location, is one of the
// schedule's.
func (s Schedule) Matches(t time.Time) bool {
	if s.minute&(1<<t.Minute()) == 0 || s.hour&(1<<t.Hour()) == 0 || s.month&(1<<int(t.Month())) == 0 {
		return false
	}
	dom := s.dom&(1<<t.Day()) != 0
	dow := s.dow&(1<<int(t.Weekday())) != 0
	if s.domStar || s.dowStar {
		return dom && dow
	}
	return dom || dow
}

// Last returns the latest scheduled minute in (after, upto], in loc, and
// whether there is one. It looks back at most 8 days.
func (s Schedule) Last(after, upto time.Time, loc *time.Location) (time.Time, bool) {
	t := upto.Truncate(time.Minute)
	floor := upto.Add(-8 * 24 * time.Hour)
	if after.After(floor) {
		floor = after
	}
	for ; t.After(floor); t = t.Add(-time.Minute) {
		if s.Matches(t.In(loc)) {
			return t, true
		}
	}
	return time.Time{}, false
}

// Next returns the first scheduled minute after t, in loc, looking ahead at
// most 400 days, and whether there is one (February 30th never comes).
func (s Schedule) Next(t time.Time, loc *time.Location) (time.Time, bool) {
	t = t.Truncate(time.Minute).Add(time.Minute)
	end := t.Add(400 * 24 * time.Hour)
	for ; t.Before(end); t = t.Add(time.Minute) {
		lt := t.In(loc)
		// Skip whole hours and days that cannot match.
		if s.month&(1<<int(lt.Month())) == 0 || !s.dayMatches(lt) {
			t = t.Add(time.Duration(23-lt.Hour())*time.Hour + time.Duration(59-lt.Minute())*time.Minute)
			continue
		}
		if s.hour&(1<<lt.Hour()) == 0 {
			t = t.Add(time.Duration(59-lt.Minute()) * time.Minute)
			continue
		}
		if s.minute&(1<<lt.Minute()) != 0 {
			return t, true
		}
	}
	return time.Time{}, false
}

func (s Schedule) dayMatches(t time.Time) bool {
	dom := s.dom&(1<<t.Day()) != 0
	dow := s.dow&(1<<int(t.Weekday())) != 0
	if s.domStar || s.dowStar {
		return dom && dow
	}
	return dom || dow
}
