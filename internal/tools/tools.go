// Package tools is Porter's tool registry: the tools each lodge app offers
// Porter through its MCP endpoint, reached over lodge's agent socket with
// that app's token, and Porter's own. Every tool is marked by what it may
// do, which is how chat and routines get different sets (ADR-0004).
package tools

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Kind is what a tool may do.
type Kind int

const (
	// Read changes nothing.
	Read Kind = iota
	// Draft makes a mail draft, which the owner reviews and sends.
	Draft
	// Write creates or edits notes; the token's scope limits where.
	Write
	// Change flags or moves mail, or makes or completes tasks and reminders.
	Change
	// Capture appends to today's daily note, only once the owner confirms.
	Capture
	// Never sends or posts to other people; it is never offered.
	Never
)

// Unattended reports whether a routine, running without the owner, may
// use a tool of kind k: reads, drafts, and notes in Porter's folder.
func (k Kind) Unattended() bool { return k == Read || k == Draft || k == Write }

// Tool is one tool as the model sees it.
type Tool struct {
	// Name is the model's name for it: "<app>__<tool>".
	Name string
	// App and Tool name it in its app.
	App, Tool string
	// Description is the app's description.
	Description string
	// Schema is its input's JSON Schema.
	Schema jsontext.Value
	// Kind is what it may do.
	Kind Kind
}

// Result is what a tool returned.
type Result struct {
	// Text is what the model reads: the structured result as JSON when
	// there is one, else the text content.
	Text string
	// Error reports a tool error, such as a write outside the token's scope.
	Error bool
	// URL is the lodge address the result names at its top level, if any.
	URL string
}

// maxResult bounds what one result gives the model.
const maxResult = 24 << 10

// Name joins an app and a tool into the model's name.
func Name(app, tool string) string { return app + "__" + tool }

// App is one lodge app's MCP endpoint, as Porter's token reaches it.
type App struct {
	// ID is the app's id, such as notes.
	ID string
	// Endpoint is its MCP URL, lodgeapp.AgentURL(ID) in production.
	Endpoint string
	// HTTP carries Porter's token for it, lodgeapp's AgentClient in
	// production.
	HTTP *http.Client
	// Version is Porter's, sent in initialize.
	Version string

	mu      sync.Mutex
	session *mcp.ClientSession
	tools   []Tool
	listed  time.Time
}

// toolsTTL is how long a tool list is kept before it is listed again.
const toolsTTL = 5 * time.Minute

func (a *App) connect(ctx context.Context) (*mcp.ClientSession, error) {
	if a.session != nil {
		return a.session, nil
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "porter", Version: a.Version}, nil)
	s, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: a.Endpoint, HTTPClient: a.HTTP,
		DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", a.ID, err)
	}
	a.session = s
	return s, nil
}

// reset drops the session, so the next use initializes a new one: after
// lodge or the app forgot it (a 404), or a connection broke.
func (a *App) reset() {
	if a.session != nil {
		_ = a.session.Close()
		a.session = nil
	}
	a.tools = nil
}

// Close ends the app's session.
func (a *App) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reset()
}

// Tools lists the app's tools, as its token's scope offers them, except
// those Porter never offers.
func (a *App) Tools(ctx context.Context) ([]Tool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tools != nil && time.Since(a.listed) < toolsTTL {
		return a.tools, nil
	}
	var ts []Tool
	err := a.retry(ctx, func(s *mcp.ClientSession) error {
		res, err := s.ListTools(ctx, nil)
		if err != nil {
			return err
		}
		ts = ts[:0]
		for _, t := range res.Tools {
			k := Classify(a.ID, t)
			if k == Never {
				continue
			}
			schema, err := json.Marshal(t.InputSchema)
			if err != nil {
				return fmt.Errorf("%s's %s schema: %w", a.ID, t.Name, err)
			}
			ts = append(ts, Tool{Name: Name(a.ID, t.Name), App: a.ID, Tool: t.Name,
				Description: t.Description, Schema: schema, Kind: k})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Name < ts[j].Name })
	a.tools, a.listed = ts, time.Now()
	return ts, nil
}

// Call calls one of the app's tools.
func (a *App) Call(ctx context.Context, tool string, args map[string]any) (Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out Result
	err := a.retry(ctx, func(s *mcp.ClientSession) error {
		res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			return err
		}
		out = result(a.ID, tool, res)
		return nil
	})
	return out, err
}

// retry runs fn on the session, and once more on a new session when the
// first attempt fails: lodge or the app may have ended the session.
func (a *App) retry(ctx context.Context, fn func(*mcp.ClientSession) error) error {
	s, err := a.connect(ctx)
	if err != nil {
		return err
	}
	if err = fn(s); err == nil || ctx.Err() != nil {
		return err
	}
	a.reset()
	s, err2 := a.connect(ctx)
	if err2 != nil {
		return errors.Join(err, err2)
	}
	return fn(s)
}

func result(app, tool string, res *mcp.CallToolResult) Result {
	var r Result
	r.Error = res.IsError
	if res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil {
			r.Text = string(b)
			r.URL = resultURL(app, tool, b)
		}
	}
	if r.Text == "" || r.Error {
		var parts []string
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				parts = append(parts, t.Text)
			}
		}
		if text := strings.Join(parts, "\n"); text != "" {
			r.Text = text
		}
	}
	if len(r.Text) > maxResult {
		r.Text = r.Text[:maxResult] + "\n[cut: the result was longer]"
	}
	return r
}
