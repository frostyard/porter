package thread

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/frostyard/porter/api"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "thread.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAddListAndPages(t *testing.T) {
	s := open(t)
	for i := range 5 {
		if _, err := s.Add(t.Context(), api.Turn{Role: api.RoleOwner, Text: "message " + string(rune('a'+i)), Done: true}); err != nil {
			t.Fatal(err)
		}
	}
	ts, more, err := s.List(t.Context(), 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !more || len(ts) != 3 || ts[0].ID != 3 || ts[2].ID != 5 {
		t.Fatalf("newest page = %v ids, more %v", ids(ts), more)
	}
	ts, more, err = s.List(t.Context(), ts[0].ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if more || len(ts) != 2 || ts[0].ID != 1 {
		t.Fatalf("older page = %v ids, more %v", ids(ts), more)
	}
	if ts[0].Cards == nil || ts[0].At == "" {
		t.Fatalf("a turn lacks cards or time: %+v", ts[0])
	}
}

func TestUpdateCardsAndFindACard(t *testing.T) {
	s := open(t)
	p, err := s.Add(t.Context(), api.Turn{Role: api.RolePorter})
	if err != nil {
		t.Fatal(err)
	}
	p.Cards = append(p.Cards, api.Card{ID: "c1", Kind: api.CardCapture, Status: api.StatusPending, App: "notes",
		Tool: "append_to_daily_note", Summary: "Capture", Text: "call the plumber"})
	p.Text, p.Done = "Shall I add this to today's note?", true
	if err := s.Update(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	got, err := s.CardTurn(t.Context(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID || len(got.Cards) != 1 || got.Cards[0].Text != "call the plumber" || !got.Done {
		t.Fatalf("card's turn = %+v", got)
	}
	if _, err := s.CardTurn(t.Context(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing card = %v", err)
	}
	if err := s.Update(t.Context(), api.Turn{ID: 99}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("updating a missing turn = %v", err)
	}
}

func TestSearchMatchesWordsNotSyntax(t *testing.T) {
	s := open(t)
	for _, text := range []string{"Dinner with Iris on Saturday", "The plumber comes Friday", `quote " and NEAR(x)`} {
		if _, err := s.Add(t.Context(), api.Turn{Role: api.RoleOwner, Text: text, Done: true}); err != nil {
			t.Fatal(err)
		}
	}
	ts, err := s.Search(t.Context(), "iris saturday", 10)
	if err != nil || len(ts) != 1 || ts[0].ID != 1 {
		t.Fatalf("search = %v, %v", ids(ts), err)
	}
	// Query syntax is not interpreted: these are words, not operators.
	if _, err := s.Search(t.Context(), `" NEAR( OR -x`, 10); err != nil {
		t.Fatalf("hostile query: %v", err)
	}
	// An edited turn is found by its new text only.
	p, _ := s.Get(t.Context(), 2)
	p.Text = "The electrician comes Friday"
	if err := s.Update(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if ts, _ := s.Search(t.Context(), "plumber", 10); len(ts) != 0 {
		t.Fatalf("old text still found: %v", ids(ts))
	}
	if ts, _ := s.Search(t.Context(), "electrician", 10); len(ts) != 1 {
		t.Fatalf("new text not found: %v", ids(ts))
	}
}

func TestSummaryAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thread.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if upto, text, err := s.Summary(t.Context()); err != nil || upto != 0 || text != "" {
		t.Fatalf("first summary = %d %q %v", upto, text, err)
	}
	if err := s.SetSummary(t.Context(), 4, "they talked about dinner"); err != nil {
		t.Fatal(err)
	}
	unfinished, _ := s.Add(t.Context(), api.Turn{Role: api.RolePorter})
	_ = s.Close()

	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if upto, text, _ := s.Summary(t.Context()); upto != 4 || text != "they talked about dinner" {
		t.Fatalf("summary after reopening = %d %q", upto, text)
	}
	got, _ := s.Get(t.Context(), unfinished.ID)
	if !got.Done || got.Text == "" {
		t.Fatalf("an unfinished reply after a restart = %+v", got)
	}
}

func ids(ts []api.Turn) []int64 {
	var out []int64
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func TestActivityAndRoutineState(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	for i := range 3 {
		if _, err := s.AddEntry(ctx, api.Entry{Kind: api.EntryRun, Routine: "Morning brief", OK: i != 1, Text: fmt.Sprint("run ", i)}); err != nil {
			t.Fatal(err)
		}
	}
	es, more, err := s.Entries(ctx, 0, 2)
	if err != nil || !more || len(es) != 2 || es[0].Text != "run 2" || es[1].OK || es[0].Cards == nil {
		t.Fatalf("entries = %+v, more %v, err %v", es, more, err)
	}
	if es, more, _ = s.Entries(ctx, es[1].ID, 2); more || len(es) != 1 || es[0].Text != "run 0" {
		t.Fatalf("the older page = %+v %v", es, more)
	}

	st, err := s.SeeRoutine(ctx, "Morning brief", time.Now())
	if err != nil || st.FirstSeen.IsZero() || !st.LastScheduled.IsZero() || st.LastOK != nil {
		t.Fatalf("a new routine's state = %+v, %v", st, err)
	}
	ran := time.Date(2026, 10, 20, 10, 30, 0, 0, time.UTC)
	if err := s.SetRan(ctx, "Morning brief", ran, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReported(ctx, "Morning brief", "h1"); err != nil {
		t.Fatal(err)
	}
	again, _ := s.SeeRoutine(ctx, "Morning brief", time.Now())
	if !again.FirstSeen.Equal(st.FirstSeen) || !again.LastScheduled.Equal(ran) || again.LastOK == nil || !*again.LastOK || again.Reported != "h1" {
		t.Fatalf("the state after a run = %+v", again)
	}
}

func TestLastSuccessIsKeptThroughFailures(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	st, err := s.SeeRoutine(ctx, "Morning brief", time.Now())
	if err != nil || !st.LastSuccess.IsZero() {
		t.Fatalf("a new routine's state = %+v, %v", st, err)
	}
	// A failure before any success leaves none.
	d1 := time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC)
	d2 := d1.Add(24 * time.Hour)
	d3 := d2.Add(24 * time.Hour)
	if err := s.SetRan(ctx, "Morning brief", d1, false); err != nil {
		t.Fatal(err)
	}
	if st, _ = s.SeeRoutine(ctx, "Morning brief", time.Now()); !st.LastSuccess.IsZero() || !st.LastScheduled.Equal(d1) {
		t.Fatalf("after a first failure = %+v", st)
	}
	if err := s.SetRan(ctx, "Morning brief", d2, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRan(ctx, "Morning brief", d3, false); err != nil {
		t.Fatal(err)
	}
	st, _ = s.SeeRoutine(ctx, "Morning brief", time.Now())
	if !st.LastSuccess.Equal(d2) || !st.LastScheduled.Equal(d3) || st.LastOK == nil || *st.LastOK {
		t.Fatalf("a failure after a success = %+v, want the success kept at %v", st, d2)
	}
}

func TestOpenAddsLastSuccessToAnOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thread.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	// The routines table as it was before last_success.
	for _, q := range []string{
		`CREATE TABLE routines (
			name           TEXT PRIMARY KEY,
			first_seen     TEXT NOT NULL,
			last_scheduled TEXT NOT NULL DEFAULT '',
			last_ok        INTEGER,
			reported       TEXT NOT NULL DEFAULT ''
		)`,
		`INSERT INTO routines (name, first_seen, last_scheduled, last_ok) VALUES ('Good', '2026-10-01T00:00:00Z', '2026-10-20T07:00:00Z', 1)`,
		`INSERT INTO routines (name, first_seen, last_scheduled, last_ok) VALUES ('Bad', '2026-10-01T00:00:00Z', '2026-10-20T07:00:00Z', 0)`,
		`INSERT INTO routines (name, first_seen) VALUES ('Never', '2026-10-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 { // the second open finds the column already there
		s, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		good, _ := s.SeeRoutine(t.Context(), "Good", time.Now())
		bad, _ := s.SeeRoutine(t.Context(), "Bad", time.Now())
		never, _ := s.SeeRoutine(t.Context(), "Never", time.Now())
		want := time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)
		if !good.LastSuccess.Equal(want) || !bad.LastSuccess.IsZero() || !never.LastSuccess.IsZero() {
			t.Fatalf("backfill: good %v, bad %v, never %v", good.LastSuccess, bad.LastSuccess, never.LastSuccess)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPendingFindsCapturesAndApprovals(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	add := func(cards ...api.Card) int64 {
		tn, err := s.Add(ctx, api.Turn{Role: api.RoleNotice, Text: "x", Cards: cards, Done: true})
		if err != nil {
			t.Fatal(err)
		}
		return tn.ID
	}
	add(api.Card{ID: "a", Kind: api.CardTool, Status: api.StatusDone})
	capture := add(api.Card{ID: "b", Kind: api.CardCapture, Status: api.StatusPending})
	add(api.Card{ID: "c", Kind: api.CardApproval, Status: api.StatusCancelled})
	approval := add(api.Card{ID: "d", Kind: api.CardTool, Status: api.StatusDone}, api.Card{ID: "e", Kind: api.CardApproval, Status: api.StatusPending})
	ts, err := s.Pending(ctx, 10)
	if err != nil || len(ts) != 2 || ts[0].ID != approval || ts[1].ID != capture {
		t.Fatalf("pending = %+v, %v", ts, err)
	}
}
