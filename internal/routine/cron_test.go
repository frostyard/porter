package routine

import (
	"testing"
	"time"
)

func TestSchedulesMatchTheirMinutes(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, ny)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		sched, at string
		want      bool
	}{
		{"30 6 * * 1-5", "2026-10-19 06:30", true},  // Monday
		{"30 6 * * 1-5", "2026-10-18 06:30", false}, // Sunday
		{"30 6 * * 1-5", "2026-10-19 06:31", false},
		{"0 18 * * 0", "2026-10-18 18:00", true},
		{"0 18 * * 7", "2026-10-18 18:00", true}, // 7 is Sunday
		{"0 18 * * sun", "2026-10-18 18:00", true},
		{"*/15 * * * *", "2026-10-19 10:45", true},
		{"*/15 * * * *", "2026-10-19 10:46", false},
		{"5/20 * * * *", "2026-10-19 10:45", true},
		{"0 9 1 * *", "2026-11-01 09:00", true},
		{"0 9 1 * mon", "2026-10-19 09:00", true},  // either day field
		{"0 9 1 * mon", "2026-10-20 09:00", false}, // neither
		{"0 9 * jan,oct *", "2026-10-20 09:00", true},
		{"0 9-17/2 * * *", "2026-10-20 15:00", true},
		{"0 9-17/2 * * *", "2026-10-20 16:00", false},
	} {
		s, err := ParseSchedule(c.sched)
		if err != nil {
			t.Fatalf("%q: %v", c.sched, err)
		}
		if got := s.Matches(at(c.at)); got != c.want {
			t.Errorf("%q at %s = %v, want %v", c.sched, c.at, got, c.want)
		}
	}
}

func TestBadSchedulesAreRefused(t *testing.T) {
	for _, s := range []string{"", "30 6 * *", "30 6 * * * *", "60 6 * * *", "30 24 * * *", "30 6 0 * *",
		"30 6 * 13 *", "30 6 * * 8", "a 6 * * *", "30 6 * * fri-mon", "*/0 * * * *", "30 6 * * 1-"} {
		if _, err := ParseSchedule(s); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
}

func TestLastAndNextFindScheduledMinutes(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	s, _ := ParseSchedule("30 6 * * 1-5")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, ny) // Tuesday noon
	last, ok := s.Last(now.Add(-24*time.Hour), now, ny)
	if !ok || !last.Equal(time.Date(2026, 10, 20, 6, 30, 0, 0, ny)) {
		t.Errorf("Last = %v %v", last, ok)
	}
	if _, ok := s.Last(now.Add(-time.Hour), now, ny); ok {
		t.Error("Last found a time in the last hour")
	}
	next, ok := s.Next(time.Date(2026, 10, 23, 7, 0, 0, 0, ny), ny) // Friday after the brief
	if !ok || !next.Equal(time.Date(2026, 10, 26, 6, 30, 0, 0, ny)) {
		t.Errorf("Next = %v %v", next, ok)
	}
	never, _ := ParseSchedule("0 9 30 2 *")
	if _, ok := never.Next(now, ny); ok {
		t.Error("February 30th came")
	}
}
