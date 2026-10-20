package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pathIn struct {
	Path string `json:"path" jsonschema:"the note's path"`
}

type noteOut struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

// fakeNotes serves an MCP server like notes': a read tool, a write tool,
// the capture tool, and a send tool Porter must never offer.
func fakeNotes(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var sessions atomic.Int32
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		sessions.Add(1)
		s := mcp.NewServer(&mcp.Implementation{Name: "fake-notes"}, nil)
		mcp.AddTool(s, &mcp.Tool{Name: "read_note", Description: "Read a note."},
			func(_ context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, noteOut, error) {
				return nil, noteOut{Path: in.Path, URL: "/notes/1/" + in.Path}, nil
			})
		mcp.AddTool(s, &mcp.Tool{Name: "create_note", Description: "Create a note."},
			func(_ context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, noteOut, error) {
				if strings.HasPrefix(in.Path, "Porter/Routines/") {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{
						Text: "invalidParams: " + in.Path + " is outside this token's writes"}}}, noteOut{}, nil
				}
				return nil, noteOut{Path: in.Path, URL: "/notes/1/" + in.Path}, nil
			})
		mcp.AddTool(s, &mcp.Tool{Name: "append_to_daily_note", Description: "Append to today's note."},
			func(_ context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, noteOut, error) {
				return nil, noteOut{URL: "/notes/1/2026-10-20.md"}, nil
			})
		mcp.AddTool(s, &mcp.Tool{Name: "send_note", Description: "Send a note to someone."},
			func(_ context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, noteOut, error) {
				t.Error("a send tool was called")
				return nil, noteOut{}, nil
			})
		return s
	}, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, &sessions
}

func notesApp(t *testing.T, url string) *App {
	a := &App{ID: "notes", Endpoint: url, HTTP: http.DefaultClient, Version: "test"}
	t.Cleanup(a.Close)
	return a
}

func TestToolsAreMarkedAndSendsNeverOffered(t *testing.T) {
	srv, _ := fakeNotes(t)
	r := NewRegistry(nil, []*App{notesApp(t, srv.URL)})
	ts, down := r.Tools(t.Context(), false)
	if len(down) != 0 {
		t.Fatalf("down = %v", down)
	}
	kinds := map[string]Kind{}
	for _, tl := range ts {
		kinds[tl.Name] = tl.Kind
	}
	want := map[string]Kind{"notes__read_note": Read, "notes__create_note": Write, "notes__append_to_daily_note": Capture}
	for n, k := range want {
		if got, ok := kinds[n]; !ok || got != k {
			t.Errorf("%s = %v (offered %v), want %v", n, got, ok, k)
		}
	}
	if _, ok := kinds["notes__send_note"]; ok {
		t.Error("a send tool was offered")
	}
	// A routine gets no capture.
	ts, _ = r.Tools(t.Context(), true)
	for _, tl := range ts {
		if tl.Kind == Capture {
			t.Errorf("a routine was offered %s", tl.Name)
		}
	}
}

func TestCallsReturnResultsAndScopeErrors(t *testing.T) {
	srv, _ := fakeNotes(t)
	r := NewRegistry(nil, []*App{notesApp(t, srv.URL)})
	tl, res, err := r.Call(t.Context(), "notes__read_note", map[string]any{"path": "Plan.md"}, false)
	if err != nil || res.Error || res.URL != "/notes/1/Plan.md" || tl.Kind != Read || !strings.Contains(res.Text, "Plan.md") {
		t.Fatalf("read = %+v %+v %v", tl, res, err)
	}
	_, res, err = r.Call(t.Context(), "notes__create_note", map[string]any{"path": "Porter/Routines/Planted.md"}, false)
	if err != nil || !res.Error || !strings.Contains(res.Text, "outside this token's writes") {
		t.Fatalf("a refused write = %+v %v", res, err)
	}
	// Names the registry does not offer are errors for the model, not calls.
	for _, name := range []string{"notes__send_note", "notes__nope", "mail__read_thread", "nonsense"} {
		_, res, err := r.Call(t.Context(), name, nil, false)
		if err != nil || !res.Error {
			t.Errorf("%s = %+v %v, want a tool error", name, res, err)
		}
	}
	// A routine may not capture, even by name.
	_, res, _ = r.Call(t.Context(), "notes__append_to_daily_note", map[string]any{"path": "x"}, true)
	if !res.Error {
		t.Error("a routine captured")
	}
}

func TestASessionTheServerForgotIsReplaced(t *testing.T) {
	srv, sessions := fakeNotes(t)
	a := notesApp(t, srv.URL)
	r := NewRegistry(nil, []*App{a})
	if _, res, err := r.Call(t.Context(), "notes__read_note", map[string]any{"path": "A.md"}, false); err != nil || res.Error {
		t.Fatalf("first call: %+v %v", res, err)
	}
	// The server forgets every session, as lodge does on eviction.
	srv.CloseClientConnections()
	a.mu.Lock()
	_ = a.session.Close() // the client's side too: its session id is now unknown
	a.mu.Unlock()
	if _, res, err := r.Call(t.Context(), "notes__read_note", map[string]any{"path": "B.md"}, false); err != nil || res.Error {
		t.Fatalf("call after the session ended: %+v %v", res, err)
	}
	if sessions.Load() < 2 {
		t.Fatalf("sessions = %d, want a new one", sessions.Load())
	}
}

func TestOwnTools(t *testing.T) {
	called := false
	own := Own{Tool: Tool{Tool: "now", Description: "The time.", Kind: Read, Schema: ObjectSchema(nil)},
		Call: func(context.Context, map[string]any) (Result, error) { called = true; return Result{Text: "noon"}, nil }}
	r := NewRegistry(nil, nil, own)
	ts, _ := r.Tools(t.Context(), true)
	if len(ts) != 1 || ts[0].Name != "porter__now" || ts[0].App != "porter" {
		t.Fatalf("own tools = %+v", ts)
	}
	if _, res, err := r.Call(t.Context(), "porter__now", nil, true); err != nil || res.Text != "noon" || !called {
		t.Fatalf("own call = %+v %v", res, err)
	}
}
