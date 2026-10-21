package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/frostyard/lodge/kit/inference"
	"github.com/frostyard/lodge/kit/kittest"
	"github.com/frostyard/lodge/kit/manifest"
	"github.com/frostyard/lodge/kit/rpctest"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/agent"
	"github.com/frostyard/porter/internal/service"
	"github.com/frostyard/porter/internal/thread"
	"github.com/frostyard/porter/internal/tools"
)

// slowModel answers every request with its reply, once gate is closed.
type slowModel struct {
	reply string
	gate  chan struct{}
}

func (m slowModel) Chat(ctx context.Context, _ inference.Priority, _ *agent.Request) (*agent.Response, error) {
	select {
	case <-m.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var res agent.Response
	err := json.Unmarshal([]byte(`{"choices":[{"message":{"role":"assistant","content":`+jsonString(m.reply)+`}}]}`), &res)
	return &res, err
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func startWith(t *testing.T, m agent.Model) (*app, *kittest.Lodge) {
	t.Helper()
	store, err := thread.Open(t.Context(), filepath.Join(t.TempDir(), "thread.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ag := &agent.Agent{Store: store, Model: m, Alias: "local", Owner: "Ann", Location: time.UTC}
	ag.Registry = tools.NewRegistry(nil, nil, agent.OwnTools(store, time.UTC, nil, nil, nil)...)
	ctx, cancel := context.WithCancel(context.Background())
	svc := service.New(ctx, store, ag, nil)
	t.Cleanup(func() { cancel(); svc.Wait() })
	a, err := newApp(slog.New(slog.DiscardHandler), svc)
	if err != nil {
		t.Fatal(err)
	}
	return a, kittest.Start(t, ID, a.lodge)
}

func start(t *testing.T) (*app, *kittest.Lodge) {
	t.Helper()
	open := make(chan struct{})
	close(open)
	return startWith(t, slowModel{reply: "Hello.", gate: open})
}

// The manifest lodge installs is valid, and names this daemon.
func installed(t *testing.T) manifest.Manifest {
	t.Helper()
	data, err := os.ReadFile("../../packaging/lodge/app.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != ID || m.Contract != manifest.Contract {
		t.Fatalf("app.json names %q, contract %d", m.ID, m.Contract)
	}
	return m
}

// The socket keeps the app contract's rules, as lodge sees it.
func TestSocketRules(t *testing.T) {
	_, l := start(t)
	kittest.CheckRules(t, l, installed(t))
}

// A browser session's /api speaks the contract, behind rpc.hello.
func TestAPI(t *testing.T) {
	_, l := start(t)
	nc, err := l.DialAPI(t.Context(), kittest.Session())
	if err != nil {
		t.Fatal(err)
	}
	c := api.NewClient(nc)
	defer c.Close()
	if err := c.RPC().Ping(t.Context(), &api.RPCPingParams{}); !errors.Is(err, api.ErrNotReady) {
		t.Errorf("ping before hello = %v, want notReady", err)
	}
	h, err := c.RPC().Hello(t.Context(), &api.RPCHelloParams{Protocol: api.Protocol, Client: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if h.Caller != api.CallerSession || h.Protocol != api.Protocol {
		t.Errorf("hello = %+v, want a session", h)
	}
	if err := c.RPC().Ping(t.Context(), &api.RPCPingParams{}); err != nil {
		t.Errorf("ping: %v", err)
	}
}

// The private socket serves local clients.
func TestPrivateSocket(t *testing.T) {
	a, _ := start(t)
	run := rpctest.Serve(t, func(ctx context.Context, ln net.Listener) error { return a.rpc.Serve(ctx, ln, a.router) })
	c, h, err := api.Dial(t.Context(), run.Socket, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if h.Caller != api.CallerLocal {
		t.Errorf("hello on the private socket = %+v, want local", h)
	}
}

// A session sends a message, and its subscribed connection sees the
// owner's turn, Porter working, its reply growing and done, and Porter
// idle; a second message while Porter replies is a conflict.
func TestAThreadOverTheSession(t *testing.T) {
	gate := make(chan struct{})
	_, l := startWith(t, slowModel{reply: "It is [in your notes](/notes/1/Plan.md).", gate: gate})
	nc, err := l.DialAPI(t.Context(), kittest.Session())
	if err != nil {
		t.Fatal(err)
	}
	c := api.NewClient(nc)
	defer c.Close()
	if _, err := c.RPC().Hello(t.Context(), &api.RPCHelloParams{Protocol: api.Protocol, Client: "test"}); err != nil {
		t.Fatal(err)
	}
	if busy, err := c.Thread().Subscribe(t.Context(), &api.ThreadSubscribeParams{}); err != nil || busy {
		t.Fatalf("subscribe = %v, %v", busy, err)
	}
	sent, err := c.Thread().Send(t.Context(), &api.ThreadSendParams{Text: "Where is my plan?",
		Context: &api.Context{App: "notes", Kind: "note", Ref: "1/Plan.md", Title: "Plan"}})
	if err != nil {
		t.Fatal(err)
	}
	if sent.Turn.Role != api.RoleOwner || sent.Turn.Context == nil || sent.Reply <= sent.Turn.ID {
		t.Fatalf("sent = %+v", sent)
	}
	if _, err := c.Thread().Send(t.Context(), &api.ThreadSendParams{Text: "again"}); err == nil {
		t.Fatal("a second message while Porter replies was accepted")
	}
	close(gate)
	var reply *api.Turn
	var sawWorking, sawIdle bool
	deadline := time.After(10 * time.Second)
	for reply == nil || !sawIdle {
		select {
		case ev := <-c.Notifications():
			switch ev.Event {
			case "turn":
				var tn api.Turn
				if err := json.Unmarshal(ev.Data, &tn); err != nil {
					t.Fatal(err)
				}
				if tn.ID == sent.Reply && tn.Done {
					reply = &tn
				}
			case "working":
				var w api.Working
				if err := json.Unmarshal(ev.Data, &w); err != nil {
					t.Fatal(err)
				}
				if w.Working {
					sawWorking = true
				} else if reply != nil {
					sawIdle = true
				}
			}
		case <-deadline:
			t.Fatalf("no done reply and idle within 10 s (reply %v, idle %v)", reply != nil, sawIdle)
		}
	}
	if !sawWorking || reply.Text != "It is [in your notes](/notes/1/Plan.md)." {
		t.Fatalf("working %v, reply %+v", sawWorking, reply)
	}
	list, err := c.Thread().List(t.Context(), &api.ThreadListParams{})
	if err != nil || len(list.Turns) != 2 || list.Turns[1].Text != reply.Text {
		t.Fatalf("list = %+v, %v", list, err)
	}
	found, err := c.Thread().Search(t.Context(), &api.ThreadSearchParams{Query: "plan"})
	if err != nil || len(found.Turns) != 2 {
		t.Fatalf("search = %+v, %v", found, err)
	}
	if err := c.Thread().Cancel(t.Context(), &api.ThreadCancelParams{Card: "nope"}); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("cancelling no card = %v", err)
	}
	if _, err := c.Thread().Send(t.Context(), &api.ThreadSendParams{Text: ""}); err == nil {
		t.Fatal("an empty message was accepted")
	}
}
