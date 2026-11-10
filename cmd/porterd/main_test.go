package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// code is an RPC error's code, or 0 for any other error.
func code(err error) api.ErrorCode {
	var e *api.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

func startWith(t *testing.T, m agent.Model) (*app, *kittest.Lodge) {
	t.Helper()
	a, l, _ := startWithStore(t, m)
	return a, l
}

func startWithStore(t *testing.T, m agent.Model) (*app, *kittest.Lodge, *thread.Store) {
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
	a, err := newApp(slog.New(slog.DiscardHandler), svc, service.NewActivity(store, nil, nil), store.PendingCount)
	if err != nil {
		t.Fatal(err)
	}
	return a, kittest.Start(t, ID, a.lodge), store
}

func start(t *testing.T) (*app, *kittest.Lodge) {
	t.Helper()
	open := make(chan struct{})
	close(open)
	return startWith(t, slowModel{reply: "Hello.", gate: open})
}

func TestHealthBadge(t *testing.T) {
	_, l, store := startWithStore(t, nil)
	check := func(want int) {
		t.Helper()
		h, err := l.Health(t.Context())
		if err != nil || !h.OK || h.Badge != want {
			t.Fatalf("health = %+v, %v; want OK, badge %d", h, err, want)
		}
	}
	check(0)
	status, body := l.Get(t, "/health", kittest.Session())
	if status != 200 || strings.Contains(body, `"badge"`) {
		t.Fatalf("empty health = %d %s; want no badge member", status, body)
	}
	tn, err := store.Add(t.Context(), api.Turn{Role: api.RolePorter, Done: true,
		Cards: []api.Card{{Kind: api.CardApproval, Status: api.StatusPending}}})
	if err != nil {
		t.Fatal(err)
	}
	check(1)
	tn.Cards[0].Status = api.StatusCancelled
	if err := store.Update(t.Context(), tn); err != nil {
		t.Fatal(err)
	}
	check(0)
}

// logBuffer keeps what a logger writes, safe for concurrent use.
type logBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func badgeLodge(t *testing.T, pending func(context.Context) (int, error)) *kittest.Lodge {
	t.Helper()
	return badgeLodgeLog(t, slog.New(slog.DiscardHandler), pending)
}

func badgeLodgeLog(t *testing.T, log *slog.Logger, pending func(context.Context) (int, error)) *kittest.Lodge {
	t.Helper()
	store, err := thread.Open(t.Context(), filepath.Join(t.TempDir(), "thread.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	svc := service.New(ctx, store, &agent.Agent{Store: store}, nil)
	t.Cleanup(func() { cancel(); svc.Wait() })
	a, err := newApp(log, svc, service.NewActivity(store, nil, nil), pending)
	if err != nil {
		t.Fatal(err)
	}
	return kittest.Start(t, ID, a.lodge)
}

func TestHealthBadgeFailure(t *testing.T) {
	l := badgeLodge(t, func(context.Context) (int, error) { return 0, errors.New("count failed") })
	h, err := l.Health(t.Context())
	if err != nil || !h.OK || h.Badge != 0 {
		t.Fatalf("health = %+v, %v; want OK without badge", h, err)
	}
}

func TestHealthBadgeNil(t *testing.T) {
	l := badgeLodge(t, nil)
	h, err := l.Health(t.Context())
	if err != nil || !h.OK || h.Badge != 0 {
		t.Fatalf("health = %+v, %v; want OK without badge", h, err)
	}
}

// A panicking count sends no badge, logs no panic value, and frees the
// counter at once: the next poll counts.
func TestHealthBadgePanic(t *testing.T) {
	var calls atomic.Int64
	var logs logBuffer
	l := badgeLodgeLog(t, slog.New(slog.NewTextHandler(&logs, nil)), func(context.Context) (int, error) {
		if calls.Add(1) == 1 {
			panic("private card text")
		}
		return 1, nil
	})
	h, err := l.Health(t.Context())
	if err != nil || !h.OK || h.Badge != 0 {
		t.Fatalf("panic health = %+v, %v; want OK without badge", h, err)
	}
	if h, err = l.Health(t.Context()); err != nil || !h.OK || h.Badge != 1 {
		t.Fatalf("health after panic = %+v, %v; want OK, badge 1", h, err)
	}
	got := logs.String()
	if !strings.Contains(got, "pending count panicked") || strings.Contains(got, "private card text") {
		t.Fatalf("logs = %q; want the panic noted without its value", got)
	}
}

func TestHealthBadgeTimeout(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	returned := make(chan struct{})
	var returnedOnce sync.Once
	var calls atomic.Int64
	l := badgeLodge(t, func(context.Context) (int, error) {
		calls.Add(1)
		<-release
		returnedOnce.Do(func() { close(returned) })
		return 7, nil
	})
	started := time.Now()
	first, cancelFirst := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelFirst()
	h, err := l.Health(first)
	if err != nil || !h.OK || h.Badge != 0 {
		t.Fatalf("slow health = %+v, %v; want OK without badge", h, err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("health took %v; want at most 2 s with scheduling margin", elapsed)
	}
	h, err = l.Health(t.Context())
	if err != nil || !h.OK || h.Badge != 0 || calls.Load() != 1 {
		t.Fatalf("busy health = %+v, %v; calls %d", h, err, calls.Load())
	}
	releaseOnce.Do(func() { close(release) })
	<-returned
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		h, err = l.Health(ctx)
		if err != nil || !h.OK {
			t.Fatalf("released health = %+v, %v", h, err)
		}
		if h.Badge == 7 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("no badge after release")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d; want 2", calls.Load())
	}
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

// Activity and the pending cards answer over a browser session, empty at
// first.
func TestActivityOverTheSession(t *testing.T) {
	_, l := start(t)
	nc, err := l.DialAPI(t.Context(), kittest.Session())
	if err != nil {
		t.Fatal(err)
	}
	c := api.NewClient(nc)
	defer c.Close()
	if _, err := c.RPC().Hello(t.Context(), &api.RPCHelloParams{Protocol: api.Protocol, Client: "test"}); err != nil {
		t.Fatal(err)
	}
	es, err := c.Activity().List(t.Context(), &api.ActivityListParams{})
	if err != nil || es.Entries == nil || len(es.Entries) != 0 || es.More {
		t.Fatalf("activity.list = %+v, %v", es, err)
	}
	if _, err := c.Activity().List(t.Context(), &api.ActivityListParams{Limit: 500}); err == nil {
		t.Error("a limit of 500 was accepted")
	}
	rs, err := c.Activity().Routines(t.Context(), &api.ActivityRoutinesParams{})
	if err != nil || rs == nil || len(rs) != 0 {
		t.Fatalf("activity.routines = %+v, %v", rs, err)
	}
	// No notes token in this daemon, so there is no scheduler to run one now.
	if err := c.Activity().Run(t.Context(), &api.ActivityRunParams{Name: "Morning brief"}); code(err) != api.CodeUnavailable {
		t.Errorf("activity.run without a scheduler = %v", err)
	}
	if err := c.Activity().Run(t.Context(), &api.ActivityRunParams{Name: ""}); code(err) != api.CodeInvalidParams {
		t.Errorf("activity.run with an empty name = %v", err)
	}
	p, err := c.Thread().Pending(t.Context(), &api.ThreadPendingParams{})
	if err != nil || len(p.Turns) != 0 {
		t.Fatalf("thread.pending = %+v, %v", p, err)
	}
}
