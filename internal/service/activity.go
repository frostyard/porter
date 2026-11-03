package service

import (
	"context"
	"strings"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/thread"
)

// Activity implements api.ActivityService over the thread store and the
// scheduler's routines.
type Activity struct {
	store    *thread.Store
	routines func(context.Context) ([]api.Routine, error)
	run      func(ctx context.Context, name string) error
}

var _ api.ActivityService = (*Activity)(nil)

// NewActivity returns the service; routines lists the routines, and run
// starts one now. Both may be nil when Porter has no notes.
func NewActivity(store *thread.Store, routines func(context.Context) ([]api.Routine, error), run func(ctx context.Context, name string) error) *Activity {
	return &Activity{store: store, routines: routines, run: run}
}

// List implements activity.list.
func (s *Activity) List(ctx context.Context, p *api.ActivityListParams) (*api.EntryList, error) {
	limit := int(p.Limit)
	switch {
	case limit == 0:
		limit = 50
	case limit < 1 || limit > 200:
		return nil, api.Errorf(api.CodeInvalidParams, "limit must be 1 to 200")
	}
	es, more, err := s.store.Entries(ctx, p.Before, limit)
	if err != nil {
		return nil, err
	}
	if es == nil {
		es = []api.Entry{}
	}
	return &api.EntryList{Entries: es, More: more}, nil
}

// Routines implements activity.routines.
func (s *Activity) Routines(ctx context.Context, _ *api.ActivityRoutinesParams) ([]api.Routine, error) {
	if s.routines == nil {
		return []api.Routine{}, nil
	}
	rs, err := s.routines(ctx)
	if rs == nil {
		rs = []api.Routine{}
	}
	return rs, err
}

// Run implements activity.run: it starts the routine named in p now.
func (s *Activity) Run(ctx context.Context, p *api.ActivityRunParams) error {
	if strings.TrimSpace(p.Name) == "" {
		return api.Errorf(api.CodeInvalidParams, "name is required")
	}
	if s.run == nil {
		return api.Errorf(api.CodeUnavailable, "the scheduler is not running")
	}
	return s.run(ctx, p.Name)
}
