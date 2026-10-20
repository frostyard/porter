package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/frostyard/lodge/kit/inference"

	"github.com/frostyard/porter/api"
)

// A routine runs with the unattended tools; a change it asks for becomes an
// approval card, which runs only when the owner approves it.
func TestARoutineProposesWhatItMayNotDo(t *testing.T) {
	r := newRig(t,
		callTool("notes__find_things", `{}`),
		callTool("notes__change_thing", `{"thing_id":16}`),
		callTool("notes__change_thing", `{"thing_id":3}`), // an id no tool returned
		say("Your brief. Changing the hiring panel waits for your approval."))
	answer, cards, err := r.agent.Run(t.Context(), RunSpec{Name: "Morning brief", Prompt: "Write my brief, and change the hiring panel thing.",
		Alias: "smart", Thinking: true, Output: "Porter/Briefs/2026-10-20.md"})
	if err != nil || !strings.HasPrefix(answer, "Your brief.") {
		t.Fatalf("Run = %q, %v", answer, err)
	}
	if len(cards) != 3 || cards[0].Status != api.StatusDone || cards[1].Kind != api.CardApproval || cards[1].Status != api.StatusPending ||
		cards[1].Args != `{"thing_id":16}` || cards[2].Status != api.StatusFailed {
		t.Fatalf("cards = %+v", cards)
	}
	if r.notes.changed.Load() != 0 {
		t.Fatal("a routine changed something")
	}
	req := r.model.requests[0]
	if r.model.prios[0] != inference.Background || req.Model != "smart" || req.TemplateArgs["enable_thinking"] != true {
		t.Errorf("the request: priority %s, model %s, thinking %v", r.model.prios[0], req.Model, req.TemplateArgs["enable_thinking"])
	}
	sys := *req.Messages[0].Content
	if !strings.Contains(sys, `You are running "Morning brief"`) || !strings.Contains(sys, "written to the note Porter/Briefs/2026-10-20.md") ||
		!strings.Contains(sys, "a routine does not see the thread") {
		t.Errorf("the system prompt:\n%s", sys)
	}
	offered := map[string]string{}
	for _, d := range req.Tools {
		offered[d.Function.Name] = d.Function.Description
	}
	if _, ok := offered["porter__remember"]; ok {
		t.Error("a routine is offered remember")
	}
	if !strings.HasPrefix(offered["notes__change_thing"], proposeNote) || !strings.HasPrefix(offered["notes__append_to_daily_note"], proposeNote) ||
		strings.HasPrefix(offered["notes__find_things"], proposeNote) || strings.HasPrefix(offered["notes__create_note"], proposeNote) {
		t.Errorf("the offered tools: %v", offered)
	}

	// The owner approves one card and dismisses another.
	run, err := r.store.Add(t.Context(), api.Turn{Role: api.RoleNotice, Text: "Morning brief ran.", Cards: cards, Done: true})
	if err != nil {
		t.Fatal(err)
	}
	n, err := r.agent.Confirm(t.Context(), cards[1].ID, "")
	if err != nil || r.notes.changed.Load() != 1 || !strings.HasPrefix(n.Text, "Approved and done") {
		t.Fatalf("approve: %+v, %v, changed %d", n, err, r.notes.changed.Load())
	}
	got, _ := r.store.Get(t.Context(), run.ID)
	if got.Cards[1].Status != api.StatusDone {
		t.Fatalf("the approved card = %+v", got.Cards[1])
	}
	if _, err := r.agent.Confirm(t.Context(), cards[1].ID, ""); !errors.Is(err, ErrNotPending) {
		t.Errorf("approving twice: %v", err)
	}
}

func TestARoutinesCaptureWaitsForApproval(t *testing.T) {
	r := newRig(t,
		callTool("notes__append_to_daily_note", `{"text":"- [ ] call Iris"}`),
		say("A capture waits for your approval."))
	_, cards, err := r.agent.Run(t.Context(), RunSpec{Name: "Inbox", Prompt: "Capture my to-dos.", Alias: "local"})
	if err != nil || len(cards) != 1 || cards[0].Kind != api.CardApproval || cards[0].Text != "- [ ] call Iris" || r.notes.appended.Load() != 0 {
		t.Fatalf("cards = %+v, err %v, appended %d", cards, err, r.notes.appended.Load())
	}
	if _, err := r.store.Add(t.Context(), api.Turn{Role: api.RoleNotice, Text: "Inbox ran.", Cards: cards, Done: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.agent.Cancel(t.Context(), cards[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.agent.Confirm(t.Context(), cards[0].ID, "edited"); !errors.Is(err, ErrNotPending) || r.notes.appended.Load() != 0 {
		t.Errorf("a dismissed approval ran: %v", err)
	}
}

func TestARoutineFailsPastTheToolLimit(t *testing.T) {
	var answers []Message
	for range MaxToolCalls + 1 {
		answers = append(answers, callTool("notes__find_things", `{}`))
	}
	r := newRig(t, answers...)
	if _, _, err := r.agent.Run(t.Context(), RunSpec{Name: "Loop", Prompt: "x", Alias: "local"}); !errors.Is(err, ErrTooManyCalls) {
		t.Fatalf("err = %v", err)
	}
}

// Text Porter writes into the vault links notes as wikilinks and the rest
// of lodge by absolute URL, so a note can follow its links.
func TestTextForTheVaultLinksSoNotesCanFollow(t *testing.T) {
	r := newRig(t,
		callTool("notes__append_to_daily_note", `{"text":"- [ ] reply to [Re: Plan](/mail/message/9), see [Plan](/notes/1/Projects/Plan.md)"}`),
		say("It waits for your confirmation."),
		callTool("porter__remember", `{"text":"Iris's thread is [Dinner](/mail/message/4)"}`),
		say("Noted."))
	r.agent.LodgeURL = "https://lodge.test"
	got := r.ask(t, "capture: reply to the plan thread", nil)
	want := "- [ ] reply to [Re: Plan](https://lodge.test/mail/message/9), see [[Projects/Plan]]"
	if len(got.Cards) != 1 || got.Cards[0].Text != want {
		t.Fatalf("the capture card = %+v", got.Cards)
	}
	r.ask(t, "remember Iris's dinner thread", nil)
	if m := r.notes.memory.Load(); m == nil || !strings.Contains(*m, "[Dinner](https://lodge.test/mail/message/4)") {
		t.Fatalf("the memory note = %v", m)
	}
}
