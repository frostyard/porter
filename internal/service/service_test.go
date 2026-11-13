package service

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostyard/lodge/kit/jsonrpc"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/agent"
)

// emitOnSubscribe answers thread.subscribe, and emits its Activity
// entries after Subscribe returns and before the answer is on the wire:
// the gap in which another request on the connection can broadcast.
type emitOnSubscribe struct {
	svc  *Thread
	emit []api.Entry
}

func (d emitOnSubscribe) Dispatch(ctx context.Context, method string, _ jsontext.Value) (any, error) {
	if method != "thread.subscribe" {
		return nil, api.Errorf(api.CodeMethodNotFound, "no %s", method)
	}
	busy, err := d.svc.Subscribe(ctx, &api.ThreadSubscribeParams{})
	for _, e := range d.emit {
		d.svc.EmitActivity(e)
	}
	return busy, err
}

// entries are Activity entries with the IDs 1 to n.
func entries(n int) []api.Entry {
	es := make([]api.Entry, n)
	for i := range es {
		es[i].ID = int64(i + 1)
	}
	return es
}

// failWrites is a connection whose writes fail while its reads go on. It
// closes failed after its first write fails.
type failWrites struct {
	net.Conn
	once   *sync.Once
	failed chan struct{}
}

func (f failWrites) Write([]byte) (int, error) {
	f.once.Do(func() { close(f.failed) })
	return 0, errors.New("write refused")
}

// serve runs the service on one connection, with d's svc set. The channel
// closes when the connection has ended and its OnClose functions have run.
// wrap, when set, wraps the server's end of the connection.
func serve(t *testing.T, d emitOnSubscribe, wrap func(net.Conn) net.Conn) (*Thread, *api.Client, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	svc := New(ctx, nil, &agent.Agent{}, nil)
	d.svc = svc
	server, client := net.Pipe()
	if wrap != nil {
		server = wrap(server)
	}
	rpc := jsonrpc.NewServer[api.ErrorCode](jsonrpc.ServerOptions{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		rpc.ServeConn(ctx, server, d)
	}()
	c := api.NewClient(client)
	t.Cleanup(func() {
		_ = c.Close()
		cancel()
		<-done
		svc.Wait()
	})
	return svc, c, done
}

// next is the ID of the next Activity event on c.
func next(t *testing.T, c *api.Client) int64 {
	t.Helper()
	select {
	case ev, ok := <-c.Notifications():
		if !ok {
			t.Fatal("the connection ended")
		}
		if ev.Event != "activity" {
			t.Fatalf("event %q, want activity", ev.Event)
		}
		var e api.Entry
		if err := json.Unmarshal(ev.Data, &e); err != nil {
			t.Fatal(err)
		}
		return e.ID
	case <-time.After(10 * time.Second):
		t.Fatal("no event within 10 s")
	}
	return 0
}

// An event broadcast after Subscribe reads busy, before its answer is on
// the wire, reaches the connection, and before any later one
// (frostyard/porter#47).
func TestSubscribeQueuesEventsUntilTheAnswer(t *testing.T) {
	svc, c, _ := serve(t, emitOnSubscribe{emit: entries(2)}, nil)
	if busy, err := c.Thread().Subscribe(t.Context(), &api.ThreadSubscribeParams{}); err != nil || busy {
		t.Fatalf("subscribe = %v, %v", busy, err)
	}
	svc.EmitActivity(api.Entry{ID: 3})
	for want := int64(1); want <= 3; want++ {
		if got := next(t, c); got != want {
			t.Fatalf("event %d, want %d", got, want)
		}
	}
}

// A second subscribe on a subscribed connection keeps one subscription:
// each event arrives once.
func TestSubscribeTwice(t *testing.T) {
	svc, c, _ := serve(t, emitOnSubscribe{}, nil)
	for range 2 {
		if _, err := c.Thread().Subscribe(t.Context(), &api.ThreadSubscribeParams{}); err != nil {
			t.Fatal(err)
		}
	}
	svc.EmitActivity(api.Entry{ID: 1})
	svc.EmitActivity(api.Entry{ID: 2})
	if a, b := next(t, c), next(t, c); a != 1 || b != 2 {
		t.Fatalf("events %d, %d; want 1, 2", a, b)
	}
}

// A connection that queues more than maxQueued events before its answer
// is closed, rather than queueing without bound, and leaves the
// subscribers.
func TestSubscribeQueueBound(t *testing.T) {
	svc, c, done := serve(t, emitOnSubscribe{emit: entries(maxQueued + 1)}, nil)
	// The close races the answer, so the answer may or may not arrive.
	_, _ = c.Thread().Subscribe(t.Context(), &api.ThreadSubscribeParams{})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection did not end within 10 s")
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.subs) != 0 {
		t.Fatalf("%d subscribers after the connection ended", len(svc.subs))
	}
}

// An event that is not sent while the queue flushes, such as one over the
// kit's message limit, does not stop the events after it.
func TestSubscribeSendsPastAnUnsentEvent(t *testing.T) {
	big := api.Entry{ID: 1, Text: strings.Repeat("x", jsonrpc.MaxMessageSize)}
	_, c, _ := serve(t, emitOnSubscribe{emit: []api.Entry{big, {ID: 2}}}, nil)
	if _, err := c.Thread().Subscribe(t.Context(), &api.ThreadSubscribeParams{}); err != nil {
		t.Fatal(err)
	}
	if got := next(t, c); got != 2 {
		t.Fatalf("event %d, want 2", got)
	}
}

// After an answer that is not written, the connection stays open but
// never goes live: its queue fills, the connection is closed, and it
// leaves the subscribers.
func TestSubscribeFailedAnswer(t *testing.T) {
	failed := make(chan struct{})
	svc, c, done := serve(t, emitOnSubscribe{},
		func(nc net.Conn) net.Conn { return failWrites{nc, new(sync.Once), failed} })
	answered := make(chan error, 1)
	go func() {
		_, err := c.Thread().Subscribe(t.Context(), &api.ThreadSubscribeParams{})
		answered <- err
	}()
	// The answer is the first write on the connection.
	select {
	case <-failed:
	case <-time.After(10 * time.Second):
		t.Fatal("no answer written within 10 s")
	}
	for _, e := range entries(maxQueued + 1) {
		svc.EmitActivity(e)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection did not end within 10 s")
	}
	if err := <-answered; err == nil {
		t.Fatal("subscribe answered over a connection whose writes fail")
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.subs) != 0 {
		t.Fatalf("%d subscribers after the connection ended", len(svc.subs))
	}
}
