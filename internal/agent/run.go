package agent

import (
	"context"
	"strings"
	"time"

	"github.com/frostyard/lodge/kit/inference"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/tools"
)

// RunSpec is one run of a routine (specs/routines.md).
type RunSpec struct {
	// Name is the routine's name.
	Name string
	// Prompt is its body: the owner's message for the run.
	Prompt string
	// Alias is the inference alias it uses.
	Alias string
	// Thinking turns the model's thinking on.
	Thinking bool
	// Output is the note the answer is written to; "" when the answer is
	// the thread line itself.
	Output string
	// LastRun is the scheduled time of the routine's last successful run;
	// zero when there is none.
	LastRun time.Time
}

// proposeNote starts the description of each tool a routine may only
// propose.
const proposeNote = "In a routine, calling this only proposes it: the owner gets an approval card, and it runs only if they approve. "

// Run runs a routine unattended: the system prompt, the memory and the
// routine's prompt, at background priority. Only the unattended tools run
// (ADR-0004); a call of any other tool offered becomes an approval card,
// and remember is not offered. It returns the final answer and the cards.
func (a *Agent) Run(ctx context.Context, spec RunSpec) (string, []api.Card, error) {
	all, down := a.Registry.Tools(ctx, false)
	var run, propose []tools.Tool
	for _, t := range all {
		switch {
		case t.Kind.Unattended():
			run = append(run, t)
		case t.Name == tools.Name("porter", "remember"):
			// Memory is written only when the owner asks.
		default:
			propose = append(propose, t)
		}
	}
	defs := append(toolDefs(run, ""), toolDefs(propose, proposeNote)...)
	p := Prompt{Owner: a.Owner, Now: a.now(), Memory: a.memory(ctx), Location: a.Location, Routine: &spec}
	sys := p.String()
	if len(down) > 0 {
		sys += "\n\nUnavailable right now: " + strings.Join(down, ", ") + ". Say so in your answer if the routine needed them."
	}
	msgs := []Message{{Role: "system", Content: text(sys)}, {Role: "user", Content: text(spec.Prompt)}}
	seen := idSet{}
	seen.addWords(spec.Prompt)
	return a.loop(ctx, msgs, defs, seen, mode{prio: inference.Background, alias: spec.Alias, thinking: spec.Thinking,
		unattended: true, what: "routine " + spec.Name})
}

// Post stores a turn of Porter's own, such as a routine's line, and shows
// it.
func (a *Agent) Post(ctx context.Context, t api.Turn) (api.Turn, error) {
	t, err := a.Store.Add(ctx, t)
	if err != nil {
		return t, err
	}
	a.emit(t)
	return t, nil
}

// Acted says whether a card is an action rather than a read: an approval,
// a capture, or a tool that is not a read. A routine's line shows only
// these.
func (a *Agent) Acted(ctx context.Context, c api.Card) bool {
	if c.Kind != api.CardTool {
		return true
	}
	t, ok := a.Registry.Lookup(ctx, tools.Name(c.App, c.Tool))
	return !ok || t.Kind != tools.Read
}
