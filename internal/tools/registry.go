package tools

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"log/slog"
	"strings"
)

// Own is one of Porter's own tools.
type Own struct {
	Tool
	// Call runs it.
	Call func(ctx context.Context, args map[string]any) (Result, error)
}

// Registry is every tool Porter may offer the model: each app's, and its
// own.
type Registry struct {
	apps []*App
	own  map[string]Own
	log  *slog.Logger
}

// NewRegistry returns a registry of apps' tools and Porter's own.
func NewRegistry(log *slog.Logger, apps []*App, own ...Own) *Registry {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	r := &Registry{apps: apps, own: map[string]Own{}, log: log}
	for _, o := range own {
		o.App = "porter"
		o.Name = Name("porter", o.Tool.Tool)
		r.own[o.Name] = o
	}
	return r
}

// App returns the app with id, or nil.
func (r *Registry) App(id string) *App {
	for _, a := range r.apps {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// Tools returns the tools to offer: in chat every tool, and for a routine
// (unattended) only those a routine may use. An app that cannot be
// reached is left out, and named in down.
func (r *Registry) Tools(ctx context.Context, unattended bool) (ts []Tool, down []string) {
	for _, a := range r.apps {
		ats, err := a.Tools(ctx)
		if err != nil {
			r.log.Warn("app's tools are unavailable", "app", a.ID, "err", err)
			down = append(down, a.ID)
			continue
		}
		for _, t := range ats {
			if !unattended || t.Kind.Unattended() {
				ts = append(ts, t)
			}
		}
	}
	for _, o := range r.own {
		if !unattended || o.Kind.Unattended() {
			ts = append(ts, o.Tool)
		}
	}
	return ts, down
}

// Call runs the tool the model named. A name the registry does not offer,
// or one a routine may not use, is an error for the model, not a call.
func (r *Registry) Call(ctx context.Context, name string, args map[string]any, unattended bool) (Tool, Result, error) {
	if o, ok := r.own[name]; ok {
		if unattended && !o.Kind.Unattended() {
			return o.Tool, Result{Error: true, Text: "This tool is not available to a routine."}, nil
		}
		res, err := o.Call(ctx, args)
		return o.Tool, res, err
	}
	app, tool, ok := strings.Cut(name, "__")
	a := r.App(app)
	if !ok || a == nil {
		return Tool{Name: name}, Result{Error: true, Text: fmt.Sprintf("There is no tool named %q.", name)}, nil
	}
	ts, err := a.Tools(ctx)
	if err != nil {
		return Tool{Name: name, App: app, Tool: tool}, Result{}, err
	}
	var t *Tool
	for i := range ts {
		if ts[i].Tool == tool {
			t = &ts[i]
		}
	}
	if t == nil {
		return Tool{Name: name, App: app, Tool: tool}, Result{Error: true, Text: fmt.Sprintf("%s offers no tool named %q.", app, tool)}, nil
	}
	if unattended && !t.Kind.Unattended() {
		return *t, Result{Error: true, Text: "This tool is not available to a routine."}, nil
	}
	res, err := a.Call(ctx, tool, args)
	return *t, res, err
}

// Lookup returns the tool named name as Call would find it, and whether
// it exists.
func (r *Registry) Lookup(ctx context.Context, name string) (Tool, bool) {
	if o, ok := r.own[name]; ok {
		return o.Tool, true
	}
	app, tool, ok := strings.Cut(name, "__")
	a := r.App(app)
	if !ok || a == nil {
		return Tool{}, false
	}
	ts, err := a.Tools(ctx)
	if err != nil {
		return Tool{}, false
	}
	for _, t := range ts {
		if t.Tool == tool {
			return t, true
		}
	}
	return Tool{}, false
}

// Close ends every app's session.
func (r *Registry) Close() {
	for _, a := range r.apps {
		a.Close()
	}
}

// ObjectSchema is a JSON Schema for an object of string properties, for
// Porter's own tools: required names first, then optional ones.
func ObjectSchema(props map[string]string, required ...string) jsontext.Value {
	var b strings.Builder
	b.WriteString(`{"type":"object","properties":{`)
	first := true
	for name, doc := range props {
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "%q:{\"type\":\"string\",\"description\":%q}", name, doc)
	}
	b.WriteString(`},"required":[`)
	for i, n := range required {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%q", n)
	}
	b.WriteString(`],"additionalProperties":false}`)
	return jsontext.Value(b.String())
}
