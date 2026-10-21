package agent

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/frostyard/lodge/kit/inference"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/thread"
	"github.com/frostyard/porter/internal/tools"
)

// MaxToolCalls bounds the tools one reply may call.
const MaxToolCalls = 40

// recentBudget is about how much of the thread, in bytes of text, goes
// into a request after the summary: some 24k tokens.
const recentBudget = 96 << 10

// MemoryPath is the owner's memory note (ADR-0005).
const MemoryPath = "Porter/Memory.md"

// Agent writes Porter's replies.
type Agent struct {
	Store    *thread.Store
	Registry *tools.Registry
	Model    Model
	// Alias is the inference alias chat uses, such as local.
	Alias string
	// Owner is the owner's name, for the prompt.
	Owner    string
	Location *time.Location
	// Emit receives every turn as it is written or updated.
	Emit func(api.Turn)
	Log  *slog.Logger
	Now  func() time.Time

	memMu   sync.Mutex
	memText string
	memAt   time.Time
	cardSeq int64
	seqMu   sync.Mutex
}

func (a *Agent) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Agent) log() *slog.Logger {
	if a.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return a.Log
}

func (a *Agent) emit(t api.Turn) {
	if a.Emit != nil {
		a.Emit(t)
	}
}

// Reply writes Porter's reply to the owner's turn into reply, a stored,
// unfinished Porter turn, updating it as tools run and finishing it.
func (a *Agent) Reply(ctx context.Context, owner api.Turn, reply api.Turn) api.Turn {
	reply, err := a.reply(ctx, owner, reply)
	if err != nil {
		a.log().Error("reply failed", "turn", reply.ID, "err", err)
		reply.Text = strings.TrimSpace(reply.Text + "\n\n" + failure(err))
	}
	reply.Done = true
	a.save(ctx, reply)
	return reply
}

func failure(err error) string {
	if errors.Is(err, context.Canceled) {
		return "(Porter stopped before finishing this reply.)"
	}
	return "I couldn't finish this reply: " + err.Error()
}

func (a *Agent) save(ctx context.Context, t api.Turn) {
	if err := a.Store.Update(context.WithoutCancel(ctx), t); err != nil {
		a.log().Error("save turn", "turn", t.ID, "err", err)
	}
	a.emit(t)
}

func (a *Agent) reply(ctx context.Context, owner api.Turn, reply api.Turn) (api.Turn, error) {
	msgs, err := a.messages(ctx, owner)
	if err != nil {
		return reply, err
	}
	offered, down := a.Registry.Tools(ctx, false)
	defs := make([]ToolDef, 0, len(offered))
	for _, t := range offered {
		defs = append(defs, ToolDef{Type: "function", Function: FuncDef{Name: t.Name, Description: t.Description, Parameters: t.Schema}})
	}
	if len(down) > 0 {
		msgs[0].Content = text(*msgs[0].Content + "\n\nUnavailable right now: " + strings.Join(down, ", ") + ". Say so if the owner asks for them.")
	}
	seen := knownIDs(owner, msgs)
	calls := 0
	nudged := false
	choice := ""
	for {
		req := &Request{Model: a.Alias, Messages: msgs, Tools: defs, ToolChoice: choice,
			TemplateArgs: map[string]any{"enable_thinking": false}}
		res, err := a.Model.Chat(ctx, inference.Interactive, req)
		if err != nil && choice != "" {
			// A provider that can't require a tool call still gets the nudge.
			a.log().Warn("the model refused tool_choice; asking without it", "err", err)
			req.ToolChoice = ""
			res, err = a.Model.Chat(ctx, inference.Interactive, req)
		}
		choice = ""
		if err != nil {
			return reply, fmt.Errorf("the model: %w", err)
		}
		if len(res.Choices) == 0 {
			return reply, errors.New("the model answered nothing")
		}
		m := res.Choices[0].Message
		if len(m.ToolCalls) == 0 {
			answer := ""
			if m.Content != nil {
				answer = strings.TrimSpace(*m.Content)
			}
			// Never let the reply claim an action no tool took: send the
			// model back once, then say so on the reply.
			if names := unbacked(answer, reply.Cards); len(names) > 0 {
				if !nudged {
					nudged = true
					a.log().Warn("a reply claimed actions no tool took; asking again", "turn", reply.ID, "claims", names)
					msgs = append(msgs, Message{Role: "assistant", Content: text(answer)},
						Message{Role: "user", Content: text(nudge(names))})
					if len(defs) > 0 {
						choice = "required"
					}
					continue
				}
				a.log().Warn("a reply still claimed actions no tool took", "turn", reply.ID, "claims", names)
				answer += unbackedNote
			}
			reply.Text = answer
			return reply, nil
		}
		msgs = append(msgs, Message{Role: "assistant", Content: m.Content, ToolCalls: m.ToolCalls})
		for _, call := range m.ToolCalls {
			calls++
			var out string
			if calls > MaxToolCalls {
				out = "Too many tool calls in one reply. Answer with what you have."
			} else {
				var card api.Card
				out, card = a.call(ctx, call, seen)
				reply.Cards = append(reply.Cards, card)
				a.save(ctx, reply)
			}
			msgs = append(msgs, Message{Role: "tool", ToolCallID: call.ID, Content: text(out)})
		}
		if calls > MaxToolCalls+4 {
			return reply, errors.New("the model kept calling tools past the limit")
		}
	}
}

// call runs one tool call and returns what the model reads, and the card.
func (a *Agent) call(ctx context.Context, call ToolCall, seen idSet) (string, api.Card) {
	card := api.Card{ID: a.cardID(), Kind: api.CardTool, Status: api.StatusDone, Tool: call.Function.Name}
	var args map[string]any
	if s := strings.TrimSpace(call.Function.Arguments); s != "" && s != "null" {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			card.Status, card.Detail, card.Summary = api.StatusFailed, "the arguments were not JSON", call.Function.Name
			return "Your arguments were not valid JSON: " + err.Error(), card
		}
	}
	card.Args = shorten(args)
	app, tool, _ := strings.Cut(call.Function.Name, "__")
	card.App, card.Tool = app, tool
	card.Summary = Summarize(tool, args)
	if app == "notes" && tool == "append_to_daily_note" {
		// Capture: shown to the owner, appended only once they confirm.
		t, _ := args["text"].(string)
		card.Kind, card.Status, card.Text = api.CardCapture, api.StatusPending, t
		card.Summary = "Append to today's daily note"
		return "This text is shown to the owner with Confirm and Cancel; nothing is appended until they confirm. Tell them it is waiting for their confirmation.", card
	}
	if tl, ok := a.Registry.Lookup(ctx, call.Function.Name); ok && tl.Kind != tools.Read {
		if bad := seen.unseen(args); len(bad) > 0 {
			slices.Sort(bad)
			card.Status, card.Detail = api.StatusFailed, "used an id no tool returned: "+strings.Join(bad, ", ")
			return "Refused, and nothing was done: " + strings.Join(bad, ", ") + " did not come from a tool result. " +
				"Find it with a search or list tool first, then use exactly the id that tool returns.", card
		}
	}
	tl, res, err := a.Registry.Call(ctx, call.Function.Name, args, false)
	if tl.App != "" {
		card.App = tl.App
	}
	switch {
	case err != nil:
		card.Status, card.Detail = api.StatusFailed, err.Error()
		return fmt.Sprintf("The tool could not be reached: %v", err), card
	case res.Error:
		card.Status, card.Detail = api.StatusFailed, firstLine(res.Text)
		return "The tool refused: " + res.Text, card
	}
	card.URL = res.URL
	seen.addJSON(res.Text)
	return wrap(card.App, card.Tool, res.Text), card
}

func (a *Agent) cardID() string {
	a.seqMu.Lock()
	defer a.seqMu.Unlock()
	a.cardSeq++
	return fmt.Sprintf("c%d-%d", a.now().UnixMilli(), a.cardSeq)
}

// messages builds the request's messages: the system prompt, the recent
// turns after the summary, and the owner's new turn.
func (a *Agent) messages(ctx context.Context, owner api.Turn) ([]Message, error) {
	upto, summary, err := a.Store.Summary(ctx)
	if err != nil {
		return nil, err
	}
	turns, err := a.Store.Since(ctx, upto)
	if err != nil {
		return nil, err
	}
	// Drop the owner's turn and anything after it; keep the newest that fit.
	// The model copies how past replies went, so a reply that claimed an
	// action no tool took is left out, with the owner's turn it answered;
	// a placeholder in its place was copied word for word.
	var past []api.Turn
	for _, t := range turns {
		if t.ID >= owner.ID {
			break
		}
		if t.Role == api.RolePorter && len(unbacked(t.Text, t.Cards)) > 0 {
			if n := len(past); n > 0 && past[n-1].Role == api.RoleOwner {
				past = past[:n-1]
			}
			continue
		}
		past = append(past, t)
	}
	size := 0
	start := len(past)
	for start > 0 && size+len(past[start-1].Text) < recentBudget {
		start--
		size += len(past[start].Text)
	}
	if start > 0 && summary == "" {
		summary = "(Earlier turns are in the thread; use search_thread to find them.)"
	}
	p := Prompt{Owner: a.Owner, Now: a.now(), Context: owner.Context, Summary: summary,
		Memory: a.memory(ctx), Location: a.Location}
	msgs := []Message{{Role: "system", Content: text(p.String())}}
	for _, t := range past[start:] {
		msgs = append(msgs, asMessages(t)...)
	}
	msgs = append(msgs, Message{Role: "user", Content: text(owner.Text)})
	return msgs, nil
}

// asMessages gives the model a past turn: an owner's as theirs, a notice
// as Porter's, and Porter's as the tool calls it made, their outcomes and
// its text.
func asMessages(t api.Turn) []Message {
	switch t.Role {
	case api.RoleOwner:
		s := t.Text
		if t.Context != nil && t.Context.Kind != "" {
			s = fmt.Sprintf("[On screen: %s]\n%s", describe(t.Context), s)
		}
		return []Message{{Role: "user", Content: text(s)}}
	case api.RolePorter:
		var out []Message
		if len(t.Cards) > 0 {
			calls := make([]ToolCall, 0, len(t.Cards))
			for _, c := range t.Cards {
				tc := ToolCall{ID: c.ID, Type: "function"}
				tc.Function.Name = c.App + "__" + c.Tool
				tc.Function.Arguments = cmp.Or(c.Args, "{}")
				calls = append(calls, tc)
			}
			out = append(out, Message{Role: "assistant", ToolCalls: calls})
			for _, c := range t.Cards {
				out = append(out, Message{Role: "tool", ToolCallID: c.ID, Content: text(outcome(c))})
			}
		}
		return append(out, Message{Role: "assistant", Content: text(t.Text)})
	}
	return []Message{{Role: "assistant", Content: text("[Notice] " + t.Text)}}
}

// outcome is what the history says a past card's tool returned; the full
// result is not kept.
func outcome(c api.Card) string {
	if c.Kind == api.CardCapture {
		switch c.Status {
		case api.StatusDone:
			return "The owner confirmed it; the text was appended to the daily note."
		case api.StatusCancelled:
			return "The owner cancelled it; nothing was appended."
		case api.StatusFailed:
			return "The append failed: " + c.Detail
		}
		return "Shown to the owner to confirm; nothing appended yet."
	}
	if c.Status == api.StatusFailed {
		return "Failed: " + c.Detail
	}
	s := "Done: " + c.Summary + "."
	if c.URL != "" {
		s += " Its url: " + c.URL
	}
	return s + " (The full result is not repeated here.)"
}

// shorten is a card's arguments for the history: valid JSON, with long
// strings cut.
func shorten(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	short := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) > 400 {
			v = s[:400] + "…"
		}
		short[k] = v
	}
	b, err := json.Marshal(short)
	if err != nil {
		return ""
	}
	return string(b)
}

// memory returns the owner's memory note, read from notes at most every
// 30 seconds; empty when there is none or notes is unavailable.
func (a *Agent) memory(ctx context.Context) string {
	a.memMu.Lock()
	defer a.memMu.Unlock()
	if !a.memAt.IsZero() && a.now().Sub(a.memAt) < 30*time.Second {
		return a.memText
	}
	notes := a.Registry.App("notes")
	if notes == nil {
		return ""
	}
	res, err := notes.Call(ctx, "read_note", map[string]any{"path": MemoryPath})
	a.memAt = a.now()
	if err != nil || res.Error {
		a.memText = ""
		return ""
	}
	var n struct {
		Content string `json:"content"`
	}
	if json.Unmarshal([]byte(res.Text), &n) == nil {
		a.memText = n.Content
	}
	return a.memText
}

// ForgetMemory makes the next request read the memory note again.
func (a *Agent) ForgetMemory() {
	a.memMu.Lock()
	a.memAt = time.Time{}
	a.memMu.Unlock()
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// Summarize is a card's one line for a tool call: the tool and its most
// telling argument.
func Summarize(tool string, args map[string]any) string {
	for _, k := range []string{"path", "query", "title", "message_id", "id", "text", "from"} {
		if v, ok := args[k]; ok {
			s := fmt.Sprint(v)
			if len(s) > 60 {
				s = s[:60] + "…"
			}
			return fmt.Sprintf("%s %s", strings.ReplaceAll(tool, "_", " "), s)
		}
	}
	return strings.ReplaceAll(tool, "_", " ")
}
