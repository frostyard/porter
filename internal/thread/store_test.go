package thread

import (
	"errors"
	"path/filepath"
	"testing"

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
