package agent

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
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
	calls := 0
	nudged := false
	for {
		req := &Request{Model: a.Alias, Messages: msgs, Tools: defs,
			TemplateArgs: map[string]any{"enable_thinking": false}}
		res, err := a.Model.Chat(ctx, inference.Interactive, req)
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
				out, card = a.call(ctx, call)
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
func (a *Agent) call(ctx context.Context, call ToolCall) (string, api.Card) {
	card := api.Card{ID: a.cardID(), Kind: api.CardTool, Status: api.StatusDone, Tool: call.Function.Name}
	var args map[string]any
	if s := strings.TrimSpace(call.Function.Arguments); s != "" && s != "null" {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			card.Status, card.Detail, card.Summary = api.StatusFailed, "the arguments were not JSON", call.Function.Name
			return "Your arguments were not valid JSON: " + err.Error(), card
		}
	}
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
	var past []api.Turn
	for _, t := range turns {
		if t.ID >= owner.ID {
			break
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
		msgs = append(msgs, asMessage(t))
	}
	msgs = append(msgs, Message{Role: "user", Content: text(owner.Text)})
	return msgs, nil
}

// asMessage gives the model a past turn: an owner's as theirs, Porter's
// with a line of what it did, a notice as Porter's.
func asMessage(t api.Turn) Message {
	switch t.Role {
	case api.RoleOwner:
		s := t.Text
		if t.Context != nil && t.Context.Kind != "" {
			s = fmt.Sprintf("[On screen: %s]\n%s", describe(t.Context), s)
		}
		return Message{Role: "user", Content: text(s)}
	case api.RolePorter:
		s := t.Text
		var did []string
		for _, c := range t.Cards {
			d := c.Summary
			if c.Status != api.StatusDone {
				d += " (" + string(c.Status) + ")"
			}
			did = append(did, d)
		}
		if len(did) > 0 {
			s += "\n[Done in this reply: " + strings.Join(did, "; ") + "]"
		} else {
			s += "\n[No tool was called in this reply: nothing in it was done.]"
		}
		return Message{Role: "assistant", Content: text(s)}
	}
	return Message{Role: "assistant", Content: text("[Notice] " + t.Text)}
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
