package agent

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/thread"
	"github.com/frostyard/porter/internal/tools"
)

// OwnTools returns Porter's own tools: search_thread, now, and remember,
// which appends to the memory note through notes and only in chat.
func OwnTools(store *thread.Store, loc *time.Location, notes *tools.App, now func() time.Time, forget func()) []tools.Own {
	if now == nil {
		now = time.Now
	}
	if loc == nil {
		loc = time.Local
	}
	own := []tools.Own{
		{Tool: tools.Tool{Tool: "search_thread", Kind: tools.Read,
			Description: "Search this whole conversation with the owner, including what the summary left out. Returns matching turns, newest first, Porter's with the tools it called.",
			Schema:      tools.ObjectSchema(map[string]string{"query": "Words to find."}, "query")},
			Call: func(ctx context.Context, args map[string]any) (tools.Result, error) {
				q, _ := args["query"].(string)
				ts, err := store.Search(ctx, q, 10)
				if err != nil {
					return tools.Result{}, err
				}
				type hit struct {
					ID     int64    `json:"id"`
					At     string   `json:"at"`
					Role   string   `json:"role"`
					Text   string   `json:"text"`
					Called []string `json:"called,omitzero"`
				}
				hits := []hit{}
				for _, t := range ts {
					// A reply that claimed what no tool did is left out here
					// too, as in the history.
					if t.Role == api.RolePorter && len(unbacked(t.Text, t.Cards)) > 0 {
						continue
					}
					s := t.Text
					if len(s) > 600 {
						s = s[:600] + "…"
					}
					h := hit{ID: t.ID, At: t.At, Role: string(t.Role), Text: s}
					for _, c := range t.Cards {
						h.Called = append(h.Called, outcome(c))
					}
					hits = append(hits, h)
				}
				b, err := json.Marshal(map[string]any{"turns": hits})
				return tools.Result{Text: string(b)}, err
			}},
		{Tool: tools.Tool{Tool: "now", Kind: tools.Read,
			Description: "The current date and time in the owner's time zone.",
			Schema:      tools.ObjectSchema(nil)},
			Call: func(context.Context, map[string]any) (tools.Result, error) {
				t := now().In(loc)
				return tools.Result{Text: fmt.Sprintf("%s (%s)", t.Format("Monday 2006-01-02 15:04"), loc)}, nil
			}},
	}
	if notes == nil {
		return own
	}
	own = append(own, tools.Own{Tool: tools.Tool{Tool: "remember", Kind: tools.Change,
		Description: "Add one line to the owner's memory note (" + MemoryPath + "). Only when the owner asks you to remember something.",
		Schema:      tools.ObjectSchema(map[string]string{"text": "What to remember, as one plain line."}, "text")},
		Call: func(ctx context.Context, args map[string]any) (tools.Result, error) {
			line, _ := args["text"].(string)
			line = strings.TrimSpace(strings.ReplaceAll(line, "\n", " "))
			if line == "" {
				return tools.Result{Error: true, Text: "Nothing to remember: text is empty."}, nil
			}
			res, err := notes.Call(ctx, "edit_note", map[string]any{"path": MemoryPath, "append": "- " + line + "\n"})
			if err == nil && res.Error && missing(res.Text) {
				res, err = notes.Call(ctx, "create_note", map[string]any{"path": MemoryPath,
					"content": "# Memory\n\nWhat the owner asked Porter to remember.\n\n- " + line + "\n"})
			}
			if forget != nil {
				forget()
			}
			return res, err
		}})
	return own
}

func missing(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "not found") || strings.Contains(s, "notfound") || strings.Contains(s, "does not exist") || strings.Contains(s, "no such")
}

// Confirm confirms a pending card: a capture's text, or the owner's edit
// of it, is appended to today's daily note, and an approval's action runs.
// It returns the notice turn it writes.
func (a *Agent) Confirm(ctx context.Context, cardID, edited string) (api.Turn, error) {
	t, i, err := a.pending(ctx, cardID)
	if err != nil {
		return api.Turn{}, err
	}
	c := &t.Cards[i]
	edited = a.ForVault(edited)
	if c.Kind == api.CardApproval {
		return a.approve(ctx, t, i, edited)
	}
	if edited != "" {
		c.Text = edited
	}
	notes := a.Registry.App("notes")
	if notes == nil {
		return api.Turn{}, ErrUnavailable
	}
	res, err := notes.Call(ctx, "append_to_daily_note", map[string]any{"text": c.Text})
	switch {
	case err != nil:
		c.Status, c.Detail = api.StatusFailed, err.Error()
	case res.Error:
		c.Status, c.Detail = api.StatusFailed, firstLine(res.Text)
	default:
		c.Status, c.URL = api.StatusDone, res.URL
	}
	a.save(ctx, t)
	if c.Status != api.StatusDone {
		return api.Turn{}, fmt.Errorf("%w: %s", ErrUnavailable, c.Detail)
	}
	msg := "Captured to today's daily note."
	if c.URL != "" {
		msg = fmt.Sprintf("Captured to [today's daily note](%s).", c.URL)
	}
	n, err := a.Store.Add(context.WithoutCancel(ctx), api.Turn{Role: api.RoleNotice, Text: msg, Done: true})
	if err != nil {
		return api.Turn{}, err
	}
	a.emit(n)
	return n, nil
}

// approve runs a pending approval's action as the owner approved it, and
// writes a notice of the outcome. A capture proposed by a routine takes the
// owner's edit of its text, as a capture does.
func (a *Agent) approve(ctx context.Context, t api.Turn, i int, edited string) (api.Turn, error) {
	c := &t.Cards[i]
	var args map[string]any
	if c.Args != "" {
		if err := json.Unmarshal([]byte(c.Args), &args); err != nil {
			return api.Turn{}, ErrBadArgs
		}
	}
	if edited != "" && c.App == "notes" && c.Tool == "append_to_daily_note" {
		if args == nil {
			args = map[string]any{}
		}
		args["text"], c.Text = edited, edited
	}
	_, res, err := a.Registry.Call(ctx, tools.Name(c.App, c.Tool), args, false)
	switch {
	case err != nil:
		c.Status, c.Detail = api.StatusFailed, err.Error()
	case res.Error:
		c.Status, c.Detail = api.StatusFailed, firstLine(res.Text)
	default:
		c.Status, c.URL = api.StatusDone, res.URL
	}
	a.save(ctx, t)
	if c.Status != api.StatusDone {
		return api.Turn{}, fmt.Errorf("%w: %s", ErrUnavailable, c.Detail)
	}
	msg := "Approved and done: " + c.Summary + "."
	if c.URL != "" {
		msg = fmt.Sprintf("Approved and done: [%s](%s).", c.Summary, c.URL)
	}
	n, err := a.Store.Add(context.WithoutCancel(ctx), api.Turn{Role: api.RoleNotice, Text: msg, Done: true})
	if err != nil {
		return api.Turn{}, err
	}
	a.emit(n)
	return n, nil
}

// Cancel cancels a pending capture, or dismisses a pending approval.
func (a *Agent) Cancel(ctx context.Context, cardID string) error {
	t, i, err := a.pending(ctx, cardID)
	if err != nil {
		return err
	}
	t.Cards[i].Status = api.StatusCancelled
	a.save(ctx, t)
	return nil
}

// ErrNotPending means the card is not a capture or an approval waiting for
// the owner.
var ErrNotPending = errors.New("the card is not pending")

// ErrBadArgs means an approval's arguments cannot be read.
var ErrBadArgs = errors.New("the approval's arguments are not a JSON object")

// ErrUnavailable means the app could not take the capture or the approved
// action.
var ErrUnavailable = errors.New("the app is unavailable")

func (a *Agent) pending(ctx context.Context, cardID string) (api.Turn, int, error) {
	t, err := a.Store.CardTurn(ctx, cardID)
	if err != nil {
		return api.Turn{}, 0, err
	}
	for i, c := range t.Cards {
		if c.ID == cardID {
			if (c.Kind != api.CardCapture && c.Kind != api.CardApproval) || c.Status != api.StatusPending {
				return api.Turn{}, 0, ErrNotPending
			}
			return t, i, nil
		}
	}
	return api.Turn{}, 0, thread.ErrNotFound
}
