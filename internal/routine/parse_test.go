package routine

import (
	"strings"
	"testing"
	"time"
)

const brief = `---
schedule: "30 6 * * 1-5"
output: "Porter/Briefs/{{date}}.md"
---
Write my morning brief for today.
`

func TestTheSpecsRoutinesParse(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	r := Parse("Porter/Routines/Morning brief.md", brief, ny, "local")
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Name != "Morning brief" || r.Schedule != "30 6 * * 1-5" || r.Model != "local" || r.Thinking || !r.Enabled ||
		r.Zone != "America/New_York" || r.Prompt != "Write my morning brief for today.\n" {
		t.Fatalf("%+v", r)
	}
	run := time.Date(2026, 10, 20, 6, 30, 0, 0, ny)
	if got := r.OutputPath(run); got != "Porter/Briefs/2026-10-20.md" {
		t.Errorf("output = %q", got)
	}
	review := Parse("Porter/Routines/Weekly review.md", "---\nschedule: \"0 18 * * 0\"\noutput: \"Porter/Reviews/{{week}}.md\"\nthinking: true\ntimezone: Europe/Lisbon\nmodel: smart\n---\nWrite my weekly review.", ny, "local")
	if review.Err != nil || !review.Thinking || review.Model != "smart" || review.Zone != "Europe/Lisbon" {
		t.Fatalf("%+v", review)
	}
	if got := review.OutputPath(time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)); got != "Porter/Reviews/2026-W42.md" {
		t.Errorf("output = %q", got)
	}
	if !review.Due(time.Date(2026, 10, 18, 17, 0, 0, 0, time.UTC)) { // 18:00 in Lisbon
		t.Error("the review is not due at 18:00 Lisbon time")
	}
}

func TestAnInvalidRoutineSaysWhy(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	for _, c := range []struct{ content, why string }{
		{"Just a prompt.", "no frontmatter"},
		{"---\nschedule: \"30 6 * * *\"\nNo closing.", "no closing"},
		{"---\nschedule: [\n---\nx", "the frontmatter"},
		{"---\noutput: \"Porter/x.md\"\n---\nx", "schedule is missing"},
		{"---\nschedule: \"30 6 * *\"\n---\nx", "5"},
		{"---\nschedule: \"30 6 * * *\"\ntags: [a]\n---\nx", `"tags" is not a routine field`},
		{"---\nschedule: \"30 6 * * *\"\nthinking: maybe\n---\nx", "the frontmatter"},
		{"---\nschedule: \"30 6 * * *\"\ntimezone: Mars/Olympus\n---\nx", "IANA"},
		{"---\nschedule: \"30 6 * * *\"\noutput: \"Notes/x.md\"\n---\nx", "not under Porter/"},
		{"---\nschedule: \"30 6 * * *\"\noutput: \"Porter/routines/x.md\"\n---\nx", "Porter/Routines/"},
		{"---\nschedule: \"30 6 * * *\"\noutput: \"Porter/x.txt\"\n---\nx", ".md"},
		{"---\nschedule: \"30 6 * * *\"\noutput: \"Porter/../x.md\"\n---\nx", "segment"},
		{"---\nschedule: \"30 6 * * *\"\noutput: \"Porter/{{month}}.md\"\n---\nx", "placeholder"},
		{"---\nschedule: \"30 6 * * *\"\nmodel: \"Big Model\"\n---\nx", "alias"},
		{"---\nschedule: \"30 6 * * *\"\n---\n  \n", "empty"},
		{"---\nschedule: \"30 6 * * *\"\n---\n" + strings.Repeat("x", MaxPrompt+1), "over"},
	} {
		r := Parse("Porter/Routines/x.md", c.content, ny, "local")
		if r.Err == nil || !strings.Contains(r.Err.Error(), c.why) {
			t.Errorf("%q: err = %v, want %q", c.content, r.Err, c.why)
		}
		if r.Due(time.Date(2026, 10, 20, 6, 30, 0, 0, ny)) {
			t.Errorf("%q: an invalid routine is due", c.content)
		}
	}
}

func TestADisabledRoutineHasNoNextRun(t *testing.T) {
	r := Parse("Porter/Routines/x.md", "---\nschedule: \"30 6 * * *\"\nenabled: false\n---\nx", time.UTC, "local")
	if r.Err != nil || r.Enabled {
		t.Fatalf("%+v", r)
	}
	if _, ok := r.Next(time.Now()); ok {
		t.Error("a disabled routine has a next run")
	}
}
