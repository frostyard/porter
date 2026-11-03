package routine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostyard/lodge/kit/inference"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.yaml.in/yaml/v3"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/agent"
	"github.com/frostyard/porter/internal/thread"
	"github.com/frostyard/porter/internal/tools"
)

// vault is a fake of notes' MCP tools over an in-memory vault.
type vault struct {
	mu    sync.Mutex
	files map[string]string
	mod   map[string]int
	edits int
}

func (v *vault) put(path, content string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.files[path] = content
	v.mod[path]++
}

func (v *vault) get(path string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ok := v.files[path]
	return c, ok
}

type (
	listIn struct {
		Folder string `json:"folder"`
		Limit  int    `json:"limit,omitempty"`
	}
	pathIn struct {
		Path string `json:"path"`
	}
	writeIn struct {
		Path    string `json:"path"`
		Content string `json:"content,omitempty"`
		OldText string `json:"old_text,omitempty"`
		NewText string `json:"new_text,omitempty"`
	}
	entry struct {
		Path     string `json:"path"`
		Modified string `json:"modified"`
		URL      string `json:"url"`
	}
	listOut struct {
		Notes []entry `json:"notes"`
	}
	writeOut struct {
		Path string `json:"path"`
		URL  string `json:"url"`
	}
	noteOut struct {
		Path       string         `json:"path"`
		URL        string         `json:"url"`
		Hash       string         `json:"hash"`
		Content    string         `json:"content"`
		Properties map[string]any `json:"properties,omitempty"`
	}
)

func newVault(t *testing.T) (*vault, *tools.App) {
	t.Helper()
	v := &vault{files: map[string]string{}, mod: map[string]int{}}
	refuse := func(msg string) *mcp.CallToolResult {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
	}
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{Name: "fake-notes"}, nil)
		mcp.AddTool(s, &mcp.Tool{Name: "list_notes"}, func(_ context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, listOut, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			out := listOut{Notes: []entry{}}
			for p := range v.files {
				if strings.HasPrefix(p, in.Folder+"/") {
					out.Notes = append(out.Notes, entry{p, fmt.Sprint(v.mod[p]), "/notes/2/" + p})
				}
			}
			return nil, out, nil
		})
		mcp.AddTool(s, &mcp.Tool{Name: "read_note"}, func(_ context.Context, _ *mcp.CallToolRequest, in pathIn) (*mcp.CallToolResult, noteOut, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			c, ok := v.files[in.Path]
			if !ok {
				return refuse("notFound: " + in.Path + " does not exist"), noteOut{}, nil
			}
			props := map[string]any{}
			if head, _, err := splitFrontmatter(c); err == nil {
				_ = yaml.Unmarshal([]byte(head), &props)
			}
			return nil, noteOut{in.Path, "/notes/2/" + in.Path, fmt.Sprint("h", v.mod[in.Path]), c, props}, nil
		})
		mcp.AddTool(s, &mcp.Tool{Name: "create_note"}, func(_ context.Context, _ *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, writeOut, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			if _, ok := v.files[in.Path]; ok {
				return refuse("conflict: " + in.Path + " exists"), writeOut{}, nil
			}
			v.files[in.Path] = in.Content
			v.mod[in.Path]++
			return nil, writeOut{Path: in.Path, URL: "/notes/2/" + in.Path}, nil
		})
		mcp.AddTool(s, &mcp.Tool{Name: "edit_note"}, func(_ context.Context, _ *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, writeOut, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			c := v.files[in.Path]
			if strings.Count(c, in.OldText) != 1 {
				return refuse("invalidParams: old_text does not occur exactly once"), writeOut{}, nil
			}
			v.files[in.Path] = strings.Replace(c, in.OldText, in.NewText, 1)
			v.mod[in.Path]++
			v.edits++
			return nil, writeOut{Path: in.Path, URL: "/notes/2/" + in.Path}, nil
		})
		return s
	}, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	app := &tools.App{ID: "notes", Endpoint: srv.URL, HTTP: http.DefaultClient, Version: "test"}
	t.Cleanup(app.Close)
	return v, app
}

// fakeAgent runs routines from a script and posts turns to the store.
type fakeAgent struct {
	store *thread.Store
	mu    sync.Mutex
	specs []agent.RunSpec
	runs  []func(ctx context.Context) (string, []api.Card, error)
}

func (f *fakeAgent) script(fns ...func(ctx context.Context) (string, []api.Card, error)) {
	f.mu.Lock()
	f.runs = append(f.runs, fns...)
	f.mu.Unlock()
}

func (f *fakeAgent) Run(ctx context.Context, spec agent.RunSpec) (string, []api.Card, error) {
	f.mu.Lock()
	f.specs = append(f.specs, spec)
	var fn func(context.Context) (string, []api.Card, error)
	if len(f.runs) > 0 {
		fn, f.runs = f.runs[0], f.runs[1:]
	}
	f.mu.Unlock()
	if fn == nil {
		return "(no script)", nil, nil
	}
	return fn(ctx)
}

func (f *fakeAgent) Acted(_ context.Context, c api.Card) bool {
	return c.Kind != api.CardTool || !strings.HasPrefix(c.Tool, "read") && !strings.HasPrefix(c.Tool, "search")
}

func (f *fakeAgent) Post(ctx context.Context, t api.Turn) (api.Turn, error) {
	return f.store.Add(ctx, t)
}

func (f *fakeAgent) ForVault(text string) string {
	return strings.ReplaceAll(text, "](/mail/", "](https://lodge.test/mail/")
}

func (f *fakeAgent) ran() []agent.RunSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agent.RunSpec(nil), f.specs...)
}

func answer(s string, cards ...api.Card) func(context.Context) (string, []api.Card, error) {
	return func(context.Context) (string, []api.Card, error) { return s, cards, nil }
}

type rig struct {
	s     *Scheduler
	v     *vault
	a     *fakeAgent
	store *thread.Store
	now   time.Time
	ny    *time.Location
	mu    sync.Mutex
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ny, _ := time.LoadLocation("America/New_York")
	store, err := thread.Open(t.Context(), filepath.Join(t.TempDir(), "thread.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	v, app := newVault(t)
	r := &rig{v: v, a: &fakeAgent{store: store}, store: store, ny: ny, now: time.Date(2026, 10, 19, 6, 0, 0, 0, ny)}
	r.s = &Scheduler{Notes: app, Agent: r.a, Store: store, Location: ny, Model: "local",
		Now: func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }}
	return r
}

// tick moves the clock to the time given, in New York, and ticks.
func (r *rig) tick(t *testing.T, at string) {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", at, r.ny)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.now = v
	r.mu.Unlock()
	r.s.Tick(t.Context(), v)
	r.s.WaitRuns()
}

func (r *rig) entries(t *testing.T) []api.Entry {
	t.Helper()
	es, _, err := r.store.Entries(t.Context(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func (r *rig) lastTurn(t *testing.T) api.Turn {
	t.Helper()
	ts, _, err := r.store.List(t.Context(), 0, 1)
	if err != nil || len(ts) == 0 {
		t.Fatalf("no turn: %v", err)
	}
	return ts[0]
}

func TestTheBriefRunsAtItsTimeAndWritesItsNote(t *testing.T) {
	r := newRig(t)
	seen := &runningSeen{}
	r.s.OnEntry = seen.on(r.s)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.v.put("Porter/Routines/Old/Ignored.md", brief) // not directly in the folder
	r.a.script(answer("# Monday\n\nThree meetings. [Re: Plan](/mail/message/9)", api.Card{ID: "c1", Kind: api.CardTool, Tool: "read_note", Status: api.StatusDone},
		api.Card{ID: "c2", Kind: api.CardApproval, App: "mail", Tool: "set_flags", Status: api.StatusPending}))
	r.tick(t, "2026-10-19 06:29")
	if len(r.a.ran()) != 0 {
		t.Fatal("ran early")
	}
	r.tick(t, "2026-10-19 06:30")
	specs := r.a.ran()
	if len(specs) != 1 || specs[0].Name != "Morning brief" || specs[0].Output != "Porter/Briefs/2026-10-19.md" ||
		specs[0].Alias != "local" || specs[0].Prompt != "Write my morning brief for today.\n" {
		t.Fatalf("runs = %+v", specs)
	}
	out, ok := r.v.get("Porter/Briefs/2026-10-19.md")
	if !ok || !strings.HasPrefix(out, "---\nroutine: Morning brief\nrun: \"2026-10-19T06:30:00-04:00\"\n---\n# Monday") ||
		!strings.Contains(out, "[Re: Plan](https://lodge.test/mail/message/9)") {
		t.Fatalf("the output note:\n%s\nthe line: %s", out, r.lastTurn(t).Text)
	}
	turn := r.lastTurn(t)
	if turn.Role != api.RoleNotice || turn.Text != "**Morning brief**: [Porter/Briefs/2026-10-19.md](/notes/2/Porter/Briefs/2026-10-19.md) One action waits for your approval." ||
		len(turn.Cards) != 1 || turn.Cards[0].ID != "c2" {
		t.Fatalf("the thread line = %+v", turn)
	}
	es := r.entries(t)
	if len(es) != 1 || es[0].Kind != api.EntryRun || !es[0].OK || es[0].Turn != turn.ID || es[0].URL == "" || len(es[0].Cards) != 1 {
		t.Fatalf("activity = %+v", es)
	}
	rs, _ := r.s.Routines(t.Context())
	if len(rs) != 1 || !rs[0].Valid || rs[0].Last != "2026-10-19T10:30:00Z" || rs[0].LastOK == nil || !*rs[0].LastOK ||
		rs[0].Next != "2026-10-20T10:30:00Z" {
		t.Fatalf("routines = %+v", rs)
	}
	// Saturday: not due.
	r.tick(t, "2026-10-24 06:30")
	if len(r.a.ran()) != 1 {
		t.Error("the weekday brief ran on Saturday")
	}
	seen.want(t)
}

func TestAnEditedRoutineTakesEffectWithoutARestart(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.tick(t, "2026-10-19 06:00")
	r.v.put("Porter/Routines/Morning brief.md", strings.Replace(brief, "30 6", "0 7", 1))
	r.tick(t, "2026-10-19 06:30")
	if len(r.a.ran()) != 0 {
		t.Fatal("the old schedule ran")
	}
	r.tick(t, "2026-10-19 07:00")
	if len(r.a.ran()) != 1 {
		t.Fatal("the new schedule did not run")
	}
	r.v.mu.Lock()
	delete(r.v.files, "Porter/Routines/Morning brief.md")
	r.v.mu.Unlock()
	r.tick(t, "2026-10-20 07:00")
	if len(r.a.ran()) != 1 {
		t.Fatal("a removed routine ran")
	}
}

func TestAnInvalidRoutineIsReportedOncePerChangeAndNeverRuns(t *testing.T) {
	r := newRig(t)
	bad := "---\nschedule: \"30 6 * * *\"\noutput: \"Notes/x.md\"\n---\nx"
	r.v.put("Porter/Routines/Bad.md", bad)
	r.tick(t, "2026-10-19 06:29")
	r.tick(t, "2026-10-19 06:30")
	r.tick(t, "2026-10-19 06:31")
	es := r.entries(t)
	if len(r.a.ran()) != 0 || len(es) != 1 || es[0].Kind != api.EntryInvalid || !strings.Contains(es[0].Text, "not under Porter/") {
		t.Fatalf("runs %d, activity %+v", len(r.a.ran()), es)
	}
	if turn := r.lastTurn(t); !strings.Contains(turn.Text, "The routine **Bad**") || !strings.Contains(turn.Text, "won't run") {
		t.Errorf("the thread line = %q", turn.Text)
	}
	r.v.put("Porter/Routines/Bad.md", bad+" changed")
	r.tick(t, "2026-10-19 06:32")
	if es := r.entries(t); len(es) != 2 {
		t.Errorf("a changed invalid routine was not reported again: %+v", es)
	}
	rs, _ := r.s.Routines(t.Context())
	if len(rs) != 1 || rs[0].Valid || rs[0].Error == "" || rs[0].Next != "" {
		t.Errorf("routines = %+v", rs)
	}
}

func TestARoutineNeverOverlapsItself(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Often.md", "---\nschedule: \"* * * * *\"\n---\nCheck things.")
	release := make(chan struct{})
	started := make(chan struct{})
	r.a.script(func(context.Context) (string, []api.Card, error) {
		close(started)
		<-release
		return "done", nil, nil
	})
	at := time.Date(2026, 10, 19, 6, 1, 0, 0, r.ny)
	r.mu.Lock()
	r.now = at
	r.mu.Unlock()
	r.s.Tick(t.Context(), at)
	<-started
	r.s.Tick(t.Context(), at.Add(time.Minute)) // still running
	close(release)
	r.s.WaitRuns()
	var skipped, ran int
	for _, e := range r.entries(t) {
		switch e.Kind {
		case api.EntrySkipped:
			skipped++
		case api.EntryRun:
			ran++
		}
	}
	if skipped != 1 || ran != 1 || len(r.a.ran()) != 1 {
		t.Fatalf("skipped %d, ran %d, runs %d", skipped, ran, len(r.a.ran()))
	}
}

func TestMissedRunsRunOnceAtStartIfRecent(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.tick(t, "2026-10-16 06:30") // Friday: it runs, and porterd stops after
	if len(r.a.ran()) != 1 {
		t.Fatal("Friday's brief did not run")
	}
	// porterd starts again on Monday at 08:00: Monday's 06:30 was missed.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s2 := &Scheduler{Notes: r.s.Notes, Agent: r.a, Store: r.store, Location: r.ny, Model: "local",
		Now: func() time.Time { return time.Date(2026, 10, 19, 8, 0, 0, 0, r.ny) }}
	s2.Start(ctx)
	s2.WaitRuns()
	specs := r.a.ran()
	if len(specs) != 2 || specs[1].Output != "Porter/Briefs/2026-10-19.md" {
		t.Fatalf("runs = %+v", specs)
	}
	cancel()
	s2.Wait()
	// Starting again at 14:00 Tuesday: Tuesday's run is over 6 hours old.
	ctx3, cancel3 := context.WithCancel(t.Context())
	defer cancel3()
	s3 := &Scheduler{Notes: r.s.Notes, Agent: r.a, Store: r.store, Location: r.ny, Model: "local",
		Now: func() time.Time { return time.Date(2026, 10, 20, 14, 0, 0, 0, r.ny) }}
	s3.Start(ctx3)
	s3.WaitRuns()
	es := r.entries(t)
	if len(r.a.ran()) != 2 || es[0].Kind != api.EntryMissed {
		t.Fatalf("runs %d, newest entry %+v", len(r.a.ran()), es[0])
	}
	cancel3()
	s3.Wait()
}

func TestOutputReplacesOnlyItsOwnNote(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.v.put("Porter/Briefs/2026-10-19.md", "My own note.\n")
	r.a.script(answer("First."), answer("Second."))
	r.tick(t, "2026-10-19 06:30")
	if mine, _ := r.v.get("Porter/Briefs/2026-10-19.md"); mine != "My own note.\n" {
		t.Fatalf("the owner's note was replaced: %q", mine)
	}
	beside, ok := r.v.get("Porter/Briefs/2026-10-19 (2).md")
	if !ok || !strings.HasSuffix(beside, "First.\n") {
		t.Fatalf("the note beside it: %q", beside)
	}
	// A second run the same day replaces its own note.
	r.v.put("Porter/Routines/Morning brief.md", strings.Replace(brief, "30 6", "30 6,7", 1))
	r.tick(t, "2026-10-19 07:30")
	beside, _ = r.v.get("Porter/Briefs/2026-10-19 (2).md")
	if !strings.HasSuffix(beside, "Second.\n") || r.v.edits != 1 {
		t.Fatalf("the rerun: %q, edits %d", beside, r.v.edits)
	}
	if _, ok := r.v.get("Porter/Briefs/2026-10-19 (3).md"); ok {
		t.Error("the rerun wrote a third note")
	}
}

func TestAFailedRunWritesNothingAndSaysWhy(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.a.script(func(context.Context) (string, []api.Card, error) { return "", nil, agent.ErrTooManyCalls })
	r.tick(t, "2026-10-19 06:30")
	if _, ok := r.v.get("Porter/Briefs/2026-10-19.md"); ok {
		t.Fatal("a failed run wrote its note")
	}
	if turn := r.lastTurn(t); turn.Text != "**Morning brief** failed: it called more than 40 tools. Nothing was written." {
		t.Errorf("the thread line = %q", turn.Text)
	}
	if es := r.entries(t); es[0].OK {
		t.Error("the run is recorded as ok")
	}
}

func TestAnUnavailableModelIsRetriedForTwoHours(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	down := func(context.Context) (string, []api.Card, error) {
		return "", nil, fmt.Errorf("the model: %w", &inference.Error{Status: 503, Code: "provider_unavailable"})
	}
	r.a.script(down, down, answer("Late brief."))
	r.tick(t, "2026-10-19 06:30")
	r.tick(t, "2026-10-19 06:44")
	if len(r.a.ran()) != 1 || len(r.entries(t)) != 0 {
		t.Fatalf("retried early: runs %d", len(r.a.ran()))
	}
	r.tick(t, "2026-10-19 06:45")
	r.tick(t, "2026-10-19 07:00")
	if out, _ := r.v.get("Porter/Briefs/2026-10-19.md"); !strings.HasSuffix(out, "Late brief.\n") || len(r.a.ran()) != 3 {
		t.Fatalf("after retries: runs %d, note %q", len(r.a.ran()), out)
	}

	// Down past two hours: it fails.
	r2 := newRig(t)
	r2.v.put("Porter/Routines/Morning brief.md", brief)
	for range 10 {
		r2.a.script(down)
	}
	r2.tick(t, "2026-10-19 06:30")
	for m := 45; m <= 6*60; m += 15 {
		r2.tick(t, time.Date(2026, 10, 19, 6, 0, 0, 0, r2.ny).Add(time.Duration(m)*time.Minute).Format("2006-01-02 15:04"))
	}
	es := r2.entries(t)
	if len(es) != 1 || es[0].OK || !strings.Contains(es[0].Text, "unavailable for 2 hours") || len(r2.a.ran()) != 9 {
		t.Fatalf("runs %d, activity %+v", len(r2.a.ran()), es)
	}
}

func TestADisabledRoutineDoesNotRun(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", strings.Replace(brief, "---\nWrite", "enabled: false\n---\nWrite", 1))
	r.tick(t, "2026-10-19 06:30")
	if len(r.a.ran()) != 0 {
		t.Fatal("a disabled routine ran")
	}
}

func TestUnavailableKnowsTheProviderIsDown(t *testing.T) {
	if !unavailable(fmt.Errorf("x: %w", &inference.Error{Status: 503})) || unavailable(&inference.Error{Status: 404}) ||
		unavailable(errors.New("the model answered nothing")) {
		t.Error("unavailable misjudged an error")
	}
}

// A run gets the scheduled time of the last run that succeeded, not of a
// failed one after it.
func TestARunGetsTheTimeOfTheLastSuccess(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.a.script(answer("Monday."),
		func(context.Context) (string, []api.Card, error) { return "", nil, agent.ErrTooManyCalls },
		answer("Wednesday."))
	r.tick(t, "2026-10-19 06:30")
	r.tick(t, "2026-10-20 06:30")
	r.tick(t, "2026-10-21 06:30")
	specs := r.a.ran()
	if len(specs) != 3 {
		t.Fatalf("runs = %+v", specs)
	}
	monday := time.Date(2026, 10, 19, 6, 30, 0, 0, r.ny)
	if !specs[0].LastRun.IsZero() || !specs[1].LastRun.Equal(monday) || !specs[2].LastRun.Equal(monday) {
		t.Errorf("last runs = %v, %v, %v; want zero, %v, %v", specs[0].LastRun, specs[1].LastRun, specs[2].LastRun, monday, monday)
	}
}

// start starts the scheduler on a context the test ends, so that RunNow has
// a base context to run on.
func (r *rig) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() { cancel(); r.s.Wait() })
	r.s.Start(ctx)
}

// code is an RPC error's code, or 0 for any other error.
func code(err error) api.ErrorCode {
	var e *api.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

// A run now runs a routine at once, with a scheduled run's prompt, output
// and reporting, and it leaves the routine's scheduled runs alone.
func TestARunNowRunsAtOnceWithTheRightsOfAScheduledRun(t *testing.T) {
	r := newRig(t)
	seen := &runningSeen{}
	r.s.OnEntry = seen.on(r.s)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.start(t)
	r.a.script(answer("Asked for now."))
	if err := r.s.RunNow(t.Context(), "Morning brief"); err != nil {
		t.Fatal(err)
	}
	r.s.WaitRuns()
	specs := r.a.ran()
	if len(specs) != 1 || specs[0].Name != "Morning brief" || specs[0].Output != "Porter/Briefs/2026-10-19.md" ||
		specs[0].Prompt != "Write my morning brief for today.\n" || !specs[0].LastRun.IsZero() {
		t.Fatalf("runs = %+v", specs)
	}
	out, ok := r.v.get("Porter/Briefs/2026-10-19.md")
	if !ok || !strings.HasPrefix(out, "---\nroutine: Morning brief\nrun: \"2026-10-19T06:00:00-04:00\"\n---\nAsked for now.") {
		t.Fatalf("the output note:\n%s", out)
	}
	turn := r.lastTurn(t)
	if turn.Text != "**Morning brief** (run now): [Porter/Briefs/2026-10-19.md](/notes/2/Porter/Briefs/2026-10-19.md)" {
		t.Errorf("the thread line = %q", turn.Text)
	}
	es := r.entries(t)
	if len(es) != 1 || es[0].Kind != api.EntryRun || !es[0].OK || !strings.HasPrefix(es[0].Text, "Run now. Wrote Porter/Briefs/2026-10-19.md.") {
		t.Fatalf("activity = %+v", es)
	}
	// Only a scheduled run moves these.
	st, err := r.store.SeeRoutine(t.Context(), "Morning brief", r.s.now())
	if err != nil {
		t.Fatal(err)
	}
	if !st.LastScheduled.IsZero() || !st.LastSuccess.IsZero() || st.LastOK != nil {
		t.Errorf("a run now moved the routine's state: %+v", st)
	}
	seen.want(t)
}

// A run now gets the scheduled time of the last success, as a scheduled run
// does, and it does not move it.
func TestARunNowLeavesTheLastRunAndTheLastSuccess(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.start(t)
	r.a.script(answer("Monday."), answer("Now."))
	r.tick(t, "2026-10-19 06:30")
	if len(r.a.ran()) != 1 {
		t.Fatalf("runs = %+v", r.a.ran())
	}
	monday := time.Date(2026, 10, 19, 6, 30, 0, 0, r.ny)
	if err := r.s.RunNow(t.Context(), "Morning brief"); err != nil {
		t.Fatal(err)
	}
	r.s.WaitRuns()
	specs := r.a.ran()
	if len(specs) != 2 || !specs[1].LastRun.Equal(monday) {
		t.Fatalf("runs = %+v", specs)
	}
	st, err := r.store.SeeRoutine(t.Context(), "Morning brief", r.s.now())
	if err != nil {
		t.Fatal(err)
	}
	if !st.LastScheduled.Equal(monday) || !st.LastSuccess.Equal(monday) || st.LastOK == nil || !*st.LastOK {
		t.Errorf("state = %+v, want Monday's run", st)
	}
}

func TestARunNowNeedsARoutineTheSchedulerRuns(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.v.put("Porter/Routines/Bad.md", "---\nschedule: \"30 6 * * *\"\noutput: \"Notes/x.md\"\n---\nx")
	// Before Start, the scheduler does not run.
	r.s.Reload(t.Context())
	if err := r.s.RunNow(t.Context(), "Morning brief"); code(err) != api.CodeUnavailable {
		t.Errorf("before Start = %v", err)
	}
	r.start(t)
	if err := r.s.RunNow(t.Context(), "Nope"); code(err) != api.CodeNotFound {
		t.Errorf("an unknown name = %v", err)
	}
	if err := r.s.RunNow(t.Context(), "Bad"); code(err) != api.CodeConflict {
		t.Errorf("an invalid routine = %v", err)
	}
	if len(r.a.ran()) != 0 {
		t.Error("a routine that cannot run now did")
	}
	// Once porterd stops, a run now is unavailable.
	ctx, cancel := context.WithCancel(t.Context())
	s2 := &Scheduler{Notes: r.s.Notes, Agent: r.a, Store: r.store, Location: r.ny, Model: "local",
		Now: func() time.Time { return time.Date(2026, 10, 19, 6, 0, 0, 0, r.ny) }}
	s2.Start(ctx)
	cancel()
	s2.Wait()
	if err := s2.RunNow(t.Context(), "Morning brief"); code(err) != api.CodeUnavailable {
		t.Errorf("after porterd stopped = %v", err)
	}
}

func TestARunNowNeverOverlapsItself(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.start(t)
	started := make(chan struct{})
	release := make(chan struct{})
	r.a.script(func(context.Context) (string, []api.Card, error) {
		close(started)
		<-release
		return "Held.", nil, nil
	})
	if err := r.s.RunNow(t.Context(), "Morning brief"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := r.s.RunNow(t.Context(), "Morning brief"); code(err) != api.CodeConflict {
		t.Errorf("a second run now = %v", err)
	}
	rs, err := r.s.Routines(t.Context())
	if err != nil || len(rs) != 1 || !rs[0].Running {
		t.Fatalf("routines = %+v, %v", rs, err)
	}
	// Its scheduled time comes while the run now is going: that time is skipped.
	at := time.Date(2026, 10, 19, 6, 30, 0, 0, r.ny)
	r.mu.Lock()
	r.now = at
	r.mu.Unlock()
	r.s.Tick(t.Context(), at)
	close(release)
	r.s.WaitRuns()
	var skipped, ran int
	for _, e := range r.entries(t) {
		switch e.Kind {
		case api.EntrySkipped:
			skipped++
		case api.EntryRun:
			ran++
		}
	}
	if skipped != 1 || ran != 1 || len(r.a.ran()) != 1 {
		t.Fatalf("skipped %d, ran %d, runs %d", skipped, ran, len(r.a.ran()))
	}
	if rs, _ := r.s.Routines(t.Context()); rs[0].Running {
		t.Error("the routine is still marked running")
	}
}

func TestARunNowIsNotRetriedWhenTheModelIsUnavailable(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.start(t)
	r.a.script(func(context.Context) (string, []api.Card, error) {
		return "", nil, fmt.Errorf("the model: %w", &inference.Error{Status: 503, Code: "provider_unavailable"})
	})
	if err := r.s.RunNow(t.Context(), "Morning brief"); err != nil {
		t.Fatal(err)
	}
	r.s.WaitRuns()
	es := r.entries(t)
	if len(es) != 1 || es[0].OK || es[0].Text != "Run now. Failed: the model is unavailable." {
		t.Fatalf("activity = %+v", es)
	}
	if turn := r.lastTurn(t); turn.Text != "**Morning brief** (run now) failed: the model is unavailable. Nothing was written." {
		t.Errorf("the thread line = %q", turn.Text)
	}
	if len(r.a.ran()) != 1 {
		t.Fatalf("runs = %d", len(r.a.ran()))
	}
	// No retry waits for the provider.
	r.tick(t, "2026-10-19 06:15")
	if len(r.a.ran()) != 1 {
		t.Error("a run now was retried")
	}
}

func TestADisabledRoutineCanRunNow(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", strings.Replace(brief, "---\nWrite", "enabled: false\n---\nWrite", 1))
	r.start(t)
	r.a.script(answer("Asked for."))
	if err := r.s.RunNow(t.Context(), "Morning brief"); err != nil {
		t.Fatal(err)
	}
	r.s.WaitRuns()
	if len(r.a.ran()) != 1 {
		t.Fatal("a disabled routine did not run now")
	}
	if _, ok := r.v.get("Porter/Briefs/2026-10-19.md"); !ok {
		t.Error("the run now wrote no output note")
	}
}

// runningSeen records, for each run's Activity entry, whether the routines
// still said that the routine was running as the entry arrived. A view
// rereads activity.routines then, so it must not see a stuck "running".
type runningSeen struct {
	mu   sync.Mutex
	vals []bool
	err  error
}

// on returns the OnEntry func that records what s.Routines says.
func (w *runningSeen) on(s *Scheduler) func(api.Entry) {
	return func(e api.Entry) {
		if e.Kind != api.EntryRun {
			return
		}
		rs, err := s.Routines(context.Background())
		w.mu.Lock()
		defer w.mu.Unlock()
		if err != nil {
			w.err = err
			return
		}
		for _, r := range rs {
			if r.Name == e.Routine {
				w.vals = append(w.vals, r.Running)
			}
		}
	}
}

// want fails unless one run was seen, and it was not running by then.
func (w *runningSeen) want(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		t.Fatalf("routines as an entry arrived: %v", w.err)
	}
	if len(w.vals) != 1 || w.vals[0] {
		t.Errorf("the routines as a run's entry arrived showed running = %v, want [false]", w.vals)
	}
}

// A run that has recorded its entry ends before a run now that started in
// between; when it ends it must not clear that new run's mark, or a third
// run could start beside it. The first run is called directly, so the test
// knows the deferred clear has run when it returns; no sleep is needed.
func TestARunEndingLeavesANewRunNowsMark(t *testing.T) {
	r := newRig(t)
	r.v.put("Porter/Routines/Morning brief.md", brief)
	r.start(t)
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	r.a.script(answer("The first run."), func(context.Context) (string, []api.Card, error) {
		close(started)
		<-release
		return "The second run.", nil, nil
	})
	var secondErr error
	entries := 0
	r.s.OnEntry = func(e api.Entry) {
		if e.Kind != api.EntryRun || e.Routine != "Morning brief" {
			return
		}
		entries++
		if entries == 1 {
			secondErr = r.s.RunNow(context.Background(), "Morning brief")
		}
	}
	// The first run, marked running as start does, to its end.
	r.s.mu.Lock()
	rr, ok := r.s.byName("Morning brief")
	if r.s.running == nil {
		r.s.running = map[string]bool{}
	}
	r.s.running["Morning brief"] = true
	r.s.mu.Unlock()
	if !ok {
		t.Fatal("no routine to run")
	}
	r.s.run(t.Context(), rr, r.s.now(), true)
	if secondErr != nil {
		t.Fatalf("a run now of a routine whose run has ended = %v", secondErr)
	}
	<-started // the second run is in the agent, and holds the mark
	if err := r.s.RunNow(t.Context(), "Morning brief"); code(err) != api.CodeConflict {
		t.Errorf("a third run now beside the second = %v", err)
	}
	unblock()
	r.s.WaitRuns()
	if rs, err := r.s.Routines(t.Context()); err != nil || len(rs) != 1 || rs[0].Running {
		t.Errorf("after both runs: routines = %+v, %v", rs, err)
	}
}
