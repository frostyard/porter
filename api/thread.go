// What a generator writes from schema/rpc/thread.yaml, written by hand
// until the app has one. Keep it in step with the schema.

package api

import (
	"context"
	"encoding/json/jsontext"

	"github.com/frostyard/lodge/kit/jsonrpc"
)

// TurnRole is who a turn is from.
type TurnRole string

const (
	// RoleOwner is the owner's message.
	RoleOwner TurnRole = "owner"
	// RolePorter is Porter's reply: its text and the cards of what it did.
	RolePorter TurnRole = "porter"
	// RoleNotice is a single line from Porter outside a reply.
	RoleNotice TurnRole = "notice"
)

// CardKind is what a card in a turn shows.
type CardKind string

const (
	// CardTool is a tool Porter called.
	CardTool CardKind = "tool"
	// CardCapture is text Porter proposes to append to today's daily note.
	CardCapture CardKind = "capture"
	// CardApproval is an action a routine proposed but may not take
	// unattended: App, Tool and Args name it, and it runs only when the
	// owner approves it.
	CardApproval CardKind = "approval"
)

// CardStatus is where a card stands.
type CardStatus string

const (
	// StatusDone means the tool ran, the capture was appended, or the
	// approved action ran.
	StatusDone CardStatus = "done"
	// StatusFailed means the tool, the append or the approved action
	// failed; Detail says why.
	StatusFailed CardStatus = "failed"
	// StatusPending is a capture or an approval waiting for the owner.
	StatusPending CardStatus = "pending"
	// StatusCancelled is a capture the owner cancelled, or an approval
	// they dismissed.
	StatusCancelled CardStatus = "cancelled"
)

// Context is what the current app reported as on screen.
type Context struct {
	// App is the reporting app's id, such as notes.
	App string `json:"app"`
	// Kind is the app's kind of thing, such as note; empty when nothing is open.
	Kind string `json:"kind"`
	// Ref is the app's reference to it, at most 1,024 characters.
	Ref string `json:"ref"`
	// Title is its title, at most 200 characters.
	Title string `json:"title"`
}

// Card is one thing Porter did, or proposes, within a reply.
type Card struct {
	// ID is the card's id, unique in the thread.
	ID string `json:"id"`
	// Kind is what the card shows.
	Kind CardKind `json:"kind"`
	// Status is where it stands.
	Status CardStatus `json:"status"`
	// App is the tool's app, such as notes or mail; porter for Porter's own.
	App string `json:"app"`
	// Tool is the tool's name, such as read_note.
	Tool string `json:"tool"`
	// Summary is one line saying what it did or proposes.
	Summary string `json:"summary"`
	// URL is a lodge address for what it touched.
	URL string `json:"url,omitzero"`
	// Text is, for a capture, the text to append.
	Text string `json:"text,omitzero"`
	// Detail is why it failed.
	Detail string `json:"detail,omitzero"`
	// Args is the tool call's arguments as a JSON object. For an approval
	// they are whole, what it runs with; otherwise long strings are
	// shortened, and porterd replays them to the model as history.
	Args string `json:"args,omitzero"`
}

// Turn is one turn of the thread.
type Turn struct {
	// ID increases through the thread.
	ID int64 `json:"id"`
	// At is when it was written, RFC 3339 in UTC.
	At string `json:"at"`
	// Role is who it is from.
	Role TurnRole `json:"role"`
	// Text is markdown.
	Text string `json:"text"`
	// Context is, for an owner's turn, the context it was sent with.
	Context *Context `json:"context,omitzero"`
	// Cards are, for Porter's turn, what it did or proposes, in order.
	Cards []Card `json:"cards"`
	// Done is whether Porter's turn is complete.
	Done bool `json:"done"`
}

// TurnList is a page of the thread, oldest first.
type TurnList struct {
	Turns []Turn `json:"turns"`
	// More is whether older turns exist before the first.
	More bool `json:"more"`
}

// Sent is the owner's turn as stored, and the id Porter's reply will have.
type Sent struct {
	Turn  Turn  `json:"turn"`
	Reply int64 `json:"reply"`
}

// Working is the data of a "working" event: whether Porter is working on
// a reply. Events arrive as EventMethod notifications whose params are the
// kit's envelope (jsonrpc.EventEnvelope): event "turn" with a Turn as
// data, or event "working" with a Working.
type Working struct {
	Working bool `json:"working"`
}

// EventMethod is the notification method events arrive as.
const EventMethod = jsonrpc.EventMethod

// ThreadListParams holds the params of thread.list.
type ThreadListParams struct {
	// Before limits the page to turns with a smaller id; 0 for the newest.
	Before int64 `json:"before,omitzero"`
	// Limit is at most this many, 1 to 200; 0 for 50.
	Limit int64 `json:"limit,omitzero"`
}

// ThreadSendParams holds the params of thread.send.
type ThreadSendParams struct {
	// Text is the owner's message, 1 to 16,384 bytes.
	Text string `json:"text"`
	// Context is what the current app reported, if anything.
	Context *Context `json:"context,omitzero"`
}

// ThreadSearchParams holds the params of thread.search.
type ThreadSearchParams struct {
	Query string `json:"query"`
	// Limit is at most this many, 1 to 100; 0 for 20.
	Limit int64 `json:"limit,omitzero"`
}

// ThreadConfirmParams holds the params of thread.confirm.
type ThreadConfirmParams struct {
	// Card is the capture card's id.
	Card string `json:"card"`
	// Text is the text as the owner edited it; empty for the card's.
	Text string `json:"text,omitzero"`
}

// ThreadCancelParams holds the params of thread.cancel.
type ThreadCancelParams struct {
	Card string `json:"card"`
}

// ThreadSubscribeParams holds the params of thread.subscribe.
type ThreadSubscribeParams struct{}

// ThreadPendingParams holds the params of thread.pending.
type ThreadPendingParams struct{}

// ThreadService is the owner's thread with Porter.
type ThreadService interface {
	List(ctx context.Context, p *ThreadListParams) (*TurnList, error)
	Send(ctx context.Context, p *ThreadSendParams) (*Sent, error)
	Search(ctx context.Context, p *ThreadSearchParams) (*TurnList, error)
	Confirm(ctx context.Context, p *ThreadConfirmParams) (*Turn, error)
	Cancel(ctx context.Context, p *ThreadCancelParams) error
	Pending(ctx context.Context, p *ThreadPendingParams) (*TurnList, error)
	Subscribe(ctx context.Context, p *ThreadSubscribeParams) (bool, error)
}

func registerThread(r *Router, s ThreadService) {
	callers := []CallerKind{CallerLocal, CallerSession}
	r.handle("thread.list", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ThreadListParams
		if err := decodeParams(raw, &p, nil); err != nil {
			return nil, err
		}
		return s.List(ctx, &p)
	})
	r.handle("thread.send", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ThreadSendParams
		if err := decodeParams(raw, &p, []string{"text"}); err != nil {
			return nil, err
		}
		return s.Send(ctx, &p)
	})
	r.handle("thread.search", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ThreadSearchParams
		if err := decodeParams(raw, &p, []string{"query"}); err != nil {
			return nil, err
		}
		return s.Search(ctx, &p)
	})
	r.handle("thread.confirm", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ThreadConfirmParams
		if err := decodeParams(raw, &p, []string{"card"}); err != nil {
			return nil, err
		}
		return s.Confirm(ctx, &p)
	})
	r.handle("thread.cancel", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ThreadCancelParams
		if err := decodeParams(raw, &p, []string{"card"}); err != nil {
			return nil, err
		}
		return nil, s.Cancel(ctx, &p)
	})
	r.handle("thread.pending", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ThreadPendingParams
		if err := decodeParams(raw, &p, nil); err != nil {
			return nil, err
		}
		return s.Pending(ctx, &p)
	})
	r.handle("thread.subscribe", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ThreadSubscribeParams
		if err := decodeParams(raw, &p, nil); err != nil {
			return nil, err
		}
		return s.Subscribe(ctx, &p)
	})
}

// ThreadClient calls the thread methods; it implements ThreadService.
type ThreadClient struct{ c *Client }

// Thread returns the thread methods.
func (c *Client) Thread() ThreadClient { return ThreadClient{c} }

var _ ThreadService = ThreadClient{}

// List calls thread.list.
func (x ThreadClient) List(ctx context.Context, p *ThreadListParams) (*TurnList, error) {
	var r TurnList
	if err := x.c.Call(ctx, "thread.list", p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Send calls thread.send.
func (x ThreadClient) Send(ctx context.Context, p *ThreadSendParams) (*Sent, error) {
	var r Sent
	if err := x.c.Call(ctx, "thread.send", p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Search calls thread.search.
func (x ThreadClient) Search(ctx context.Context, p *ThreadSearchParams) (*TurnList, error) {
	var r TurnList
	if err := x.c.Call(ctx, "thread.search", p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Confirm calls thread.confirm.
func (x ThreadClient) Confirm(ctx context.Context, p *ThreadConfirmParams) (*Turn, error) {
	var r Turn
	if err := x.c.Call(ctx, "thread.confirm", p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Cancel calls thread.cancel.
func (x ThreadClient) Cancel(ctx context.Context, p *ThreadCancelParams) error {
	return x.c.Call(ctx, "thread.cancel", p, nil)
}

// Pending calls thread.pending.
func (x ThreadClient) Pending(ctx context.Context, p *ThreadPendingParams) (*TurnList, error) {
	var r TurnList
	if err := x.c.Call(ctx, "thread.pending", p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Subscribe calls thread.subscribe.
func (x ThreadClient) Subscribe(ctx context.Context, p *ThreadSubscribeParams) (bool, error) {
	var r bool
	if err := x.c.Call(ctx, "thread.subscribe", p, &r); err != nil {
		return false, err
	}
	return r, nil
}
