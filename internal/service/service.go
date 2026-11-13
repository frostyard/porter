// Package service implements porterd's thread domain (schema/rpc/thread.yaml)
// over the agent and the thread store: one reply at a time, and events to
// every subscribed connection as turns are written.
package service

import (
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"sync"
	"unicode/utf8"

	"github.com/frostyard/lodge/kit/jsonrpc"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/agent"
	"github.com/frostyard/porter/internal/thread"
)

// Thread implements api.ThreadService.
type Thread struct {
	store *thread.Store
	agent *agent.Agent
	log   *slog.Logger
	// base outlives any connection: replies run under it.
	base context.Context

	mu   sync.Mutex
	busy bool
	subs map[*jsonrpc.Conn]*subscriber
	wg   sync.WaitGroup
}

// maxQueued is the number of events a subscriber queues before its answer
// is on the wire. A connection that passes it is closed.
const maxQueued = 256

// subscriber is one subscribed connection. Its events queue from the
// moment thread.subscribe reads busy until the answer is on the wire, so
// none is lost in between; then they go out in order.
type subscriber struct {
	conn *jsonrpc.Conn

	mu    sync.Mutex // held while an event is written, to keep the order
	live  bool
	queue []jsonrpc.EventEnvelope
}

// deliver sends e, or queues it until the answer is on the wire.
func (s *subscriber) deliver(e jsonrpc.EventEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.live {
		if len(s.queue) >= maxQueued {
			_ = s.conn.Close()
			return errors.New("too many events before the subscribe answer")
		}
		s.queue = append(s.queue, e)
		return nil
	}
	return s.conn.Notify(api.EventMethod, e)
}

// start sends the queued events, and the later ones as they come.
func (s *subscriber) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.queue
	s.queue, s.live = nil, true
	for _, e := range q {
		if err := s.conn.Notify(api.EventMethod, e); err != nil {
			return err
		}
	}
	return nil
}

var _ api.ThreadService = (*Thread)(nil)

// New returns the service. It sets the agent's Emit to deliver events, and
// runs replies under ctx.
func New(ctx context.Context, store *thread.Store, a *agent.Agent, log *slog.Logger) *Thread {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Thread{store: store, agent: a, log: log, base: ctx, subs: map[*jsonrpc.Conn]*subscriber{}}
	a.Emit = s.emitTurn
	return s
}

// Wait waits for a reply in progress to finish.
func (s *Thread) Wait() { s.wg.Wait() }

func (s *Thread) emitTurn(t api.Turn) { s.broadcast("turn", t) }

func (s *Thread) emitWorking(w bool) { s.broadcast("working", api.Working{Working: w}) }

// broadcast sends an event to every subscribed connection, as the kit's
// envelope.
func (s *Thread) broadcast(name string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		s.log.Error("encode event", "event", name, "err", err)
		return
	}
	e := jsonrpc.EventEnvelope{Event: name, Data: raw}
	s.mu.Lock()
	subs := make([]*subscriber, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()
	for _, sub := range subs {
		if err := sub.deliver(e); err != nil {
			s.log.Debug("event not delivered", "err", err)
		}
	}
}

// List implements thread.list.
func (s *Thread) List(ctx context.Context, p *api.ThreadListParams) (*api.TurnList, error) {
	limit := int(p.Limit)
	switch {
	case limit == 0:
		limit = 50
	case limit < 1 || limit > 200:
		return nil, api.Errorf(api.CodeInvalidParams, "limit must be 1 to 200")
	}
	ts, more, err := s.store.List(ctx, p.Before, limit)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		ts = []api.Turn{}
	}
	return &api.TurnList{Turns: ts, More: more}, nil
}

// Search implements thread.search.
func (s *Thread) Search(ctx context.Context, p *api.ThreadSearchParams) (*api.TurnList, error) {
	limit := int(p.Limit)
	switch {
	case limit == 0:
		limit = 20
	case limit < 1 || limit > 100:
		return nil, api.Errorf(api.CodeInvalidParams, "limit must be 1 to 100")
	}
	ts, err := s.store.Search(ctx, p.Query, limit)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		ts = []api.Turn{}
	}
	return &api.TurnList{Turns: ts}, nil
}

// Send implements thread.send: it stores the owner's turn and an
// unfinished reply, and writes the reply in the background.
func (s *Thread) Send(ctx context.Context, p *api.ThreadSendParams) (*api.Sent, error) {
	if p.Text == "" || len(p.Text) > 16384 || !utf8.ValidString(p.Text) {
		return nil, api.Errorf(api.CodeInvalidParams, "text must be 1 to 16,384 bytes of UTF-8")
	}
	if c := p.Context; c != nil && (len(c.Ref) > 1024 || len(c.Title) > 200 || len(c.Kind) > 32 || len(c.App) > 16) {
		return nil, api.Errorf(api.CodeInvalidParams, "the context is too long")
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return nil, api.Errorf(api.CodeConflict, "Porter is still replying")
	}
	s.busy = true
	s.mu.Unlock()
	owner, err := s.store.Add(ctx, api.Turn{Role: api.RoleOwner, Text: p.Text, Context: p.Context, Done: true})
	if err != nil {
		s.idle()
		return nil, err
	}
	reply, err := s.store.Add(ctx, api.Turn{Role: api.RolePorter})
	if err != nil {
		s.idle()
		return nil, err
	}
	// The owner's turn, then working, then the reply that grows.
	s.emitTurn(owner)
	s.emitWorking(true)
	s.emitTurn(reply)
	s.wg.Go(func() {
		defer s.idle()
		s.agent.Reply(s.base, owner, reply)
		if err := s.agent.Compact(s.base); err != nil {
			s.log.Warn("the summary was not updated", "err", err)
		}
	})
	return &api.Sent{Turn: owner, Reply: reply.ID}, nil
}

func (s *Thread) idle() {
	s.mu.Lock()
	was := s.busy
	s.busy = false
	s.mu.Unlock()
	if was {
		s.emitWorking(false)
	}
}

// Confirm implements thread.confirm.
func (s *Thread) Confirm(ctx context.Context, p *api.ThreadConfirmParams) (*api.Turn, error) {
	n, err := s.agent.Confirm(ctx, p.Card, p.Text)
	if err != nil {
		return nil, cardError(err)
	}
	return &n, nil
}

// Cancel implements thread.cancel.
func (s *Thread) Cancel(ctx context.Context, p *api.ThreadCancelParams) error {
	return cardError(s.agent.Cancel(ctx, p.Card))
}

func cardError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, thread.ErrNotFound):
		return api.Errorf(api.CodeNotFound, "no such card")
	case errors.Is(err, agent.ErrNotPending):
		return api.Errorf(api.CodeConflict, "the card is not pending")
	case errors.Is(err, agent.ErrBadArgs):
		return api.Errorf(api.CodeInvalidParams, "%v", err)
	case errors.Is(err, agent.ErrUnavailable):
		return api.Errorf(api.CodeUnavailable, "%v", err)
	}
	return err
}

// Pending implements thread.pending.
func (s *Thread) Pending(ctx context.Context, _ *api.ThreadPendingParams) (*api.TurnList, error) {
	ts, err := s.store.Pending(ctx, 100)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		ts = []api.Turn{}
	}
	return &api.TurnList{Turns: ts}, nil
}

// EmitActivity sends an Activity entry to every subscribed connection.
func (s *Thread) EmitActivity(e api.Entry) { s.broadcast("activity", e) }

// Subscribe implements thread.subscribe: every event from the moment it
// reads busy reaches this connection, after the answer. The connection
// joins the subscribers under the same lock as the read, so a change of
// busy after the answer comes as an event.
func (s *Thread) Subscribe(ctx context.Context, _ *api.ThreadSubscribeParams) (bool, error) {
	c := jsonrpc.ConnFrom(ctx)
	if c == nil {
		return false, api.Errorf(api.CodeInvalidRequest, "events need a connection")
	}
	s.mu.Lock()
	busy := s.busy
	if _, ok := s.subs[c]; ok {
		s.mu.Unlock()
		return busy, nil // subscribed already: events flow
	}
	sub := &subscriber{conn: c}
	s.subs[c] = sub
	s.mu.Unlock()
	// Outside the lock: on a closed connection, OnClose runs fn at once.
	// A failed answer means a broken connection, which this removes.
	c.OnClose(func() {
		s.mu.Lock()
		delete(s.subs, c)
		s.mu.Unlock()
	})
	jsonrpc.AfterReply(ctx, func() {
		if err := sub.start(); err != nil {
			s.log.Debug("event not delivered", "err", err)
		}
	})
	return busy, nil
}
