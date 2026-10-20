// What a generator writes from schema/rpc/activity.yaml, written by hand
// until the app has one. Keep it in step with the schema.

package api

import (
	"context"
	"encoding/json/jsontext"
)

// EntryKind is what an Activity entry records.
type EntryKind string

const (
	// EntryRun is a routine's run: OK says whether it succeeded, URL links
	// its output note, and Cards are what it did or proposes.
	EntryRun EntryKind = "run"
	// EntrySkipped is a routine time that came while its last run was still
	// going.
	EntrySkipped EntryKind = "skipped"
	// EntryMissed is a routine time porterd was down for, more than 6
	// hours ago, so it did not run.
	EntryMissed EntryKind = "missed"
	// EntryInvalid is a routine that is invalid and does not run.
	EntryInvalid EntryKind = "invalid"
)

// Entry is one thing Porter did or noticed on its own.
type Entry struct {
	// ID increases.
	ID int64 `json:"id"`
	// At is when it was recorded, RFC 3339 in UTC.
	At string `json:"at"`
	// Kind is what it records.
	Kind EntryKind `json:"kind"`
	// Routine is the routine's name, its file name without .md.
	Routine string `json:"routine"`
	// OK is, for a run, whether it succeeded.
	OK bool `json:"ok"`
	// Text is one line: what happened, or why.
	Text string `json:"text"`
	// URL is, for a run, a lodge address of its output note.
	URL string `json:"url,omitzero"`
	// Turn is the thread turn that reported it.
	Turn int64 `json:"turn,omitzero"`
	// Cards are, for a run, what it did or proposes, as they stand now.
	Cards []Card `json:"cards"`
}

// EntryList is a page of Activity, newest first.
type EntryList struct {
	// Entries are newest first.
	Entries []Entry `json:"entries"`
	// More says whether older entries exist after the last.
	More bool `json:"more"`
}

// Routine is a routine as porterd last read it from Porter/Routines/.
type Routine struct {
	// Name is the file name without .md.
	Name string `json:"name"`
	// Path is its note's path in the vault.
	Path string `json:"path"`
	// URL is a lodge address of its note.
	URL string `json:"url,omitzero"`
	// Valid says whether it parses and passes every rule.
	Valid bool `json:"valid"`
	// Error is why it is invalid.
	Error string `json:"error,omitzero"`
	// Schedule is its five cron fields, as written.
	Schedule string `json:"schedule"`
	// Timezone is the zone its schedule is in.
	Timezone string `json:"timezone"`
	// Output is its output path template.
	Output string `json:"output,omitzero"`
	// Enabled says whether it runs.
	Enabled bool `json:"enabled"`
	// Next is when it runs next, RFC 3339 in UTC.
	Next string `json:"next,omitzero"`
	// Last is when it last ran, RFC 3339 in UTC.
	Last string `json:"last,omitzero"`
	// LastOK is whether its last run succeeded.
	LastOK *bool `json:"lastOk,omitzero"`
}

// ActivityListParams holds the params of activity.list.
type ActivityListParams struct {
	Before int64 `json:"before,omitzero"`
	Limit  int64 `json:"limit,omitzero"`
}

// ActivityRoutinesParams holds the params of activity.routines.
type ActivityRoutinesParams struct{}

// ActivityService is what Porter does on its own.
type ActivityService interface {
	List(ctx context.Context, p *ActivityListParams) (*EntryList, error)
	Routines(ctx context.Context, p *ActivityRoutinesParams) ([]Routine, error)
}

func registerActivity(r *Router, s ActivityService) {
	callers := []CallerKind{CallerLocal, CallerSession}
	r.handle("activity.list", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ActivityListParams
		if err := decodeParams(raw, &p, nil); err != nil {
			return nil, err
		}
		return s.List(ctx, &p)
	})
	r.handle("activity.routines", callers, func(ctx context.Context, raw jsontext.Value) (any, error) {
		var p ActivityRoutinesParams
		if err := decodeParams(raw, &p, nil); err != nil {
			return nil, err
		}
		return s.Routines(ctx, &p)
	})
}

// ActivityClient calls the activity methods; it implements ActivityService.
type ActivityClient struct{ c *Client }

// Activity returns the activity methods.
func (c *Client) Activity() ActivityClient { return ActivityClient{c} }

var _ ActivityService = ActivityClient{}

// List calls activity.list.
func (x ActivityClient) List(ctx context.Context, p *ActivityListParams) (*EntryList, error) {
	var r EntryList
	if err := x.c.Call(ctx, "activity.list", p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Routines calls activity.routines.
func (x ActivityClient) Routines(ctx context.Context, p *ActivityRoutinesParams) ([]Routine, error) {
	var r []Routine
	if err := x.c.Call(ctx, "activity.routines", p, &r); err != nil {
		return nil, err
	}
	return r, nil
}
