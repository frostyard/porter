package routine

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/frostyard/lodge/kit/inference"
	"go.yaml.in/yaml/v3"

	"github.com/frostyard/porter/api"
	"github.com/frostyard/porter/internal/agent"
	"github.com/frostyard/porter/internal/thread"
	"github.com/frostyard/porter/internal/tools"
)

// Notes is what the scheduler needs of notes: its MCP tools, through
// Porter's token.
type Notes interface {
	Call(ctx context.Context, tool string, args map[string]any) (tools.Result, error)
}

// Agent is what the scheduler needs of the agent.
type Agent interface {
	Run(ctx context.Context, spec agent.RunSpec) (string, []api.Card, error)
	Acted(ctx context.Context, c api.Card) bool
	Post(ctx context.Context, t api.Turn) (api.Turn, error)
}

// The spec's limits (specs/routines.md, Rules).
const (
	RunTimeout = 10 * time.Minute
	RetryEvery = 15 * time.Minute
	RetryFor   = 2 * time.Hour
	MissWindow = 6 * time.Hour
)

// Scheduler loads routines from notes, runs each when it is due, writes
// its output note, and records Activity.
type Scheduler struct {
	Notes Notes
	Agent Agent
	Store *thread.Store
	// Location and Model are the defaults for a routine that names neither.
	Location *time.Location
	Model    string
	// OnEntry, if set, sees each Activity entry once it is stored.
	OnEntry func(api.Entry)
	Log     *slog.Logger
	Now     func() time.Time

	mu       sync.Mutex
	routines map[string]Routine // by path
	listed   map[string]string  // path → modified, as list_notes gave it
	running  map[string]bool    // by name
	retries  map[string]retry   // by name
	lastTick time.Time
	runs     sync.WaitGroup
	loop     sync.WaitGroup
}

// retry is a run waiting for the model's provider to come back.
type retry struct {
	r         Routine
	scheduled time.Time
	at        time.Time
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Scheduler) log() *slog.Logger {
	if s.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Log
}

// Start reads the routines, runs any missed while porterd was down (less
// than 6 hours ago), and then checks every minute until ctx ends.
func (s *Scheduler) Start(ctx context.Context) {
	now := s.now()
	s.Reload(ctx)
	s.catchUp(ctx, now)
	s.mu.Lock()
	s.lastTick = now
	s.mu.Unlock()
	s.loop.Go(func() {
		for {
			next := time.Now().Truncate(time.Minute).Add(time.Minute)
			t := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			s.Tick(ctx, s.now())
		}
	})
}

// Wait waits for the minute loop to end and for runs in progress.
func (s *Scheduler) Wait() {
	s.loop.Wait()
	s.runs.Wait()
}

// WaitRuns waits for the runs in progress.
func (s *Scheduler) WaitRuns() { s.runs.Wait() }

// Tick rereads changed routines, starts each routine due since the last
// tick, and retries runs whose provider was unavailable.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) {
	s.Reload(ctx)
	s.mu.Lock()
	after := s.lastTick
	if after.IsZero() || now.Sub(after) > 8*24*time.Hour {
		after = now.Add(-time.Minute)
	}
	s.lastTick = now
	rs := s.sorted()
	var again []retry
	for name, rt := range s.retries {
		if !rt.at.After(now) {
			again = append(again, rt)
			delete(s.retries, name)
		}
	}
	s.mu.Unlock()
	for _, rt := range again {
		s.launch(ctx, rt.r, rt.scheduled)
	}
	for _, r := range rs {
		if !r.Valid() || !r.Enabled {
			continue
		}
		t, ok := r.Last(after, now)
		if !ok {
			continue
		}
		if now.Sub(t) >= MissWindow {
			s.record(ctx, api.Entry{Kind: api.EntryMissed, Routine: r.Name,
				Text: fmt.Sprintf("Not run: due at %s, more than 6 hours ago.", s.clock(t, r))})
			continue
		}
		s.start(ctx, r, t)
	}
}

// catchUp runs, once, each routine whose last scheduled time passed while
// porterd was down, if that was less than 6 hours ago; an older miss is
// noted.
func (s *Scheduler) catchUp(ctx context.Context, now time.Time) {
	s.mu.Lock()
	rs := s.sorted()
	s.mu.Unlock()
	for _, r := range rs {
		if !r.Valid() || !r.Enabled {
			continue
		}
		st, err := s.Store.SeeRoutine(ctx, r.Name, s.now())
		if err != nil {
			s.log().Error("routine state", "routine", r.Name, "err", err)
			continue
		}
		from := st.FirstSeen
		if st.LastScheduled.After(from) {
			from = st.LastScheduled
		}
		t, ok := r.Last(from, now)
		if !ok {
			continue
		}
		if now.Sub(t) >= MissWindow {
			s.record(ctx, api.Entry{Kind: api.EntryMissed, Routine: r.Name,
				Text: fmt.Sprintf("Not run: porterd was down at %s, more than 6 hours ago.", s.clock(t, r))})
			if err := s.Store.SetRan(ctx, r.Name, t, false); err != nil {
				s.log().Error("record a miss", "routine", r.Name, "err", err)
			}
			continue
		}
		s.log().Info("running a routine missed while porterd was down", "routine", r.Name, "due", t)
		s.start(ctx, r, t)
	}
}

// start runs r for its time t, unless its last run is still going.
func (s *Scheduler) start(ctx context.Context, r Routine, t time.Time) {
	s.mu.Lock()
	if s.running[r.Name] {
		s.mu.Unlock()
		s.record(ctx, api.Entry{Kind: api.EntrySkipped, Routine: r.Name,
			Text: fmt.Sprintf("Skipped %s: the last run was still going.", s.clock(t, r))})
		return
	}
	if s.running == nil {
		s.running = map[string]bool{}
	}
	s.running[r.Name] = true
	s.mu.Unlock()
	s.launch(ctx, r, t)
}

// launch runs r in the background; r is marked running.
func (s *Scheduler) launch(ctx context.Context, r Routine, t time.Time) {
	s.runs.Go(func() { s.run(ctx, r, t) })
}

func (s *Scheduler) run(ctx context.Context, r Routine, scheduled time.Time) {
	rctx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()
	output := r.OutputPath(scheduled)
	answer, cards, err := s.Agent.Run(rctx, agent.RunSpec{Name: r.Name, Prompt: r.Prompt, Alias: r.Model, Thinking: r.Thinking, Output: output})
	if err != nil && unavailable(err) && ctx.Err() == nil {
		if now := s.now(); now.Add(RetryEvery).Sub(scheduled) <= RetryFor {
			s.log().Warn("the model is unavailable; the routine will retry", "routine", r.Name, "in", RetryEvery, "err", err)
			s.mu.Lock()
			if s.retries == nil {
				s.retries = map[string]retry{}
			}
			s.retries[r.Name] = retry{r: r, scheduled: scheduled, at: now.Add(RetryEvery)}
			s.mu.Unlock()
			return // still running: its next times are skipped while it waits
		}
		err = fmt.Errorf("the model was unavailable for 2 hours: %w", err)
	}
	defer func() {
		s.mu.Lock()
		delete(s.running, r.Name)
		s.mu.Unlock()
	}()
	if ctx.Err() != nil {
		return // porterd is stopping; the run is retried as missed at start
	}
	var acts []api.Card
	for _, c := range cards {
		if s.Agent.Acted(ctx, c) {
			acts = append(acts, c)
		}
	}
	url, written := "", ""
	if err == nil && output != "" {
		if url, written, err = s.write(ctx, r, output, scheduled, answer); err != nil {
			err = fmt.Errorf("the output note could not be written: %w", err)
		}
	}
	line, text := s.lines(r, answer, url, written, acts, err)
	turn, perr := s.Agent.Post(context.WithoutCancel(ctx), api.Turn{Role: api.RoleNotice, Text: line, Cards: acts, Done: true})
	if perr != nil {
		s.log().Error("post a routine's line", "routine", r.Name, "err", perr)
	}
	s.record(ctx, api.Entry{Kind: api.EntryRun, Routine: r.Name, OK: err == nil, Text: text, URL: url, Turn: turn.ID, Cards: acts})
	if err := s.Store.SetRan(context.WithoutCancel(ctx), r.Name, scheduled, err == nil); err != nil {
		s.log().Error("record a run", "routine", r.Name, "err", err)
	}
	if err != nil {
		s.log().Warn("a routine failed", "routine", r.Name, "err", err)
	} else {
		s.log().Info("a routine ran", "routine", r.Name, "output", written, "actions", len(acts))
	}
}

// lines are a run's thread line and its Activity text.
func (s *Scheduler) lines(r Routine, answer, url, written string, acts []api.Card, err error) (string, string) {
	if err != nil {
		why := err.Error()
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			why = "it ran past 10 minutes"
		case errors.Is(err, agent.ErrTooManyCalls):
			why = "it called more than 40 tools"
		}
		return fmt.Sprintf("**%s** failed: %s. Nothing was written.", r.Name, why), "Failed: " + why + "."
	}
	waiting := 0
	for _, c := range acts {
		if c.Kind == api.CardApproval && c.Status == api.StatusPending {
			waiting++
		}
	}
	tail := ""
	switch waiting {
	case 0:
	case 1:
		tail = " One action waits for your approval."
	default:
		tail = fmt.Sprintf(" %d actions wait for your approval.", waiting)
	}
	if written == "" {
		return fmt.Sprintf("**%s**\n\n%s%s", r.Name, answer, tail), "Ran." + tail
	}
	link := written
	if url != "" {
		link = fmt.Sprintf("[%s](%s)", written, url)
	}
	return fmt.Sprintf("**%s**: %s%s", r.Name, link, tail), "Wrote " + written + "." + tail
}

// outputHead is an output note's frontmatter.
type outputHead struct {
	Routine string `yaml:"routine"`
	Run     string `yaml:"run"`
}

// write writes a run's answer to its output note at p, replacing a note
// there only when its frontmatter names the same routine, and otherwise
// writing beside it with " (2)" and so on before .md. It returns the note's
// address and path.
func (s *Scheduler) write(ctx context.Context, r Routine, p string, scheduled time.Time, answer string) (string, string, error) {
	head, err := yaml.Marshal(outputHead{Routine: r.Name, Run: scheduled.In(r.Location()).Format(time.RFC3339)})
	if err != nil {
		return "", "", err
	}
	content := "---\n" + string(head) + "---\n" + strings.TrimSpace(answer) + "\n"
	for n := 1; n <= 20; n++ {
		at := p
		if n > 1 {
			at = strings.TrimSuffix(p, ".md") + fmt.Sprintf(" (%d).md", n)
		}
		res, err := s.Notes.Call(ctx, "read_note", map[string]any{"path": at})
		if err != nil {
			return "", "", err
		}
		if res.Error {
			if !strings.Contains(res.Text, "does not exist") {
				return "", "", errors.New(firstLine(res.Text))
			}
			res, err := s.Notes.Call(ctx, "create_note", map[string]any{"path": at, "content": content})
			if err := toolErr(res, err); err != nil {
				return "", "", err
			}
			return res.URL, at, nil
		}
		var note struct {
			Content    string         `json:"content"`
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal([]byte(res.Text), &note); err != nil {
			return "", "", fmt.Errorf("read %s: %w", at, err)
		}
		if note.Properties["routine"] != r.Name {
			continue // someone else's note: write beside it
		}
		res, err = s.Notes.Call(ctx, "edit_note", map[string]any{"path": at, "old_text": note.Content, "new_text": content})
		if err := toolErr(res, err); err != nil {
			return "", "", err
		}
		return res.URL, at, nil
	}
	return "", "", fmt.Errorf("%s and 19 notes beside it belong to something else", p)
}

func toolErr(res tools.Result, err error) error {
	if err != nil {
		return err
	}
	if res.Error {
		return errors.New(firstLine(res.Text))
	}
	return nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// unavailable says whether err means the model's provider or lodge's
// inference socket is down, so the run should be retried.
func unavailable(err error) bool {
	var ie *inference.Error
	if errors.As(err, &ie) {
		return ie.Status == 503 || ie.Status == 502 || ie.Status == 504 || ie.Status == 429
	}
	var op *net.OpError
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) || errors.As(err, &op)
}

// record stores an Activity entry and passes it on.
func (s *Scheduler) record(ctx context.Context, e api.Entry) {
	e, err := s.Store.AddEntry(context.WithoutCancel(ctx), e)
	if err != nil {
		s.log().Error("record activity", "routine", e.Routine, "err", err)
		return
	}
	if s.OnEntry != nil {
		s.OnEntry(e)
	}
}

// clock is t as the routine's owner reads it.
func (s *Scheduler) clock(t time.Time, r Routine) string {
	return t.In(r.Location()).Format("Mon 2006-01-02 15:04")
}

// sorted returns the routines by name; s.mu is held.
func (s *Scheduler) sorted() []Routine {
	rs := make([]Routine, 0, len(s.routines))
	for _, r := range s.routines {
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
	return rs
}

// listEntry is one note list_notes gives.
type listEntry struct {
	Path     string `json:"path"`
	Modified string `json:"modified"`
}

// Reload rereads the routines that changed since the last read, through
// list_notes and read_note. Without notes, the routines stay as they were.
func (s *Scheduler) Reload(ctx context.Context) {
	if s.Notes == nil {
		return
	}
	res, err := s.Notes.Call(ctx, "list_notes", map[string]any{"folder": strings.TrimSuffix(Folder, "/"), "limit": 1000})
	if err := toolErr(res, err); err != nil {
		s.log().Warn("routines could not be listed", "err", err)
		return
	}
	var list struct {
		Notes []listEntry `json:"notes"`
	}
	if err := json.Unmarshal([]byte(res.Text), &list); err != nil {
		s.log().Warn("routines could not be listed", "err", err)
		return
	}
	seen := map[string]bool{}
	for _, n := range list.Notes {
		rest, ok := strings.CutPrefix(n.Path, Folder)
		if !ok || strings.Contains(rest, "/") || !strings.HasSuffix(rest, ".md") {
			continue // not directly in the folder
		}
		seen[n.Path] = true
		s.mu.Lock()
		same := s.listed[n.Path] == n.Modified && s.routines[n.Path].Path != ""
		s.mu.Unlock()
		if !same {
			s.read(ctx, n)
		}
	}
	s.mu.Lock()
	for p := range s.routines {
		if !seen[p] {
			s.log().Info("a routine was removed", "path", p)
			delete(s.routines, p)
			delete(s.listed, p)
		}
	}
	s.mu.Unlock()
}

// read reads and parses one routine note, and reports it once if invalid.
func (s *Scheduler) read(ctx context.Context, n listEntry) {
	res, err := s.Notes.Call(ctx, "read_note", map[string]any{"path": n.Path})
	if err := toolErr(res, err); err != nil {
		s.log().Warn("a routine could not be read", "path", n.Path, "err", err)
		return
	}
	var note struct {
		Content string `json:"content"`
		Hash    string `json:"hash"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal([]byte(res.Text), &note); err != nil {
		s.log().Warn("a routine could not be read", "path", n.Path, "err", err)
		return
	}
	r := Parse(n.Path, note.Content, s.Location, s.Model)
	r.Hash, r.URL = note.Hash, note.URL
	s.mu.Lock()
	if s.routines == nil {
		s.routines, s.listed = map[string]Routine{}, map[string]string{}
	}
	s.routines[n.Path], s.listed[n.Path] = r, n.Modified
	s.mu.Unlock()
	s.log().Info("read a routine", "routine", r.Name, "valid", r.Valid())
	st, err := s.Store.SeeRoutine(ctx, r.Name, s.now())
	if err != nil {
		s.log().Error("routine state", "routine", r.Name, "err", err)
		return
	}
	if r.Valid() || st.Reported == r.Hash {
		return
	}
	link := r.Path
	if r.URL != "" {
		link = fmt.Sprintf("[%s](%s)", r.Path, r.URL)
	}
	turn, err := s.Agent.Post(ctx, api.Turn{Role: api.RoleNotice, Done: true,
		Text: fmt.Sprintf("The routine **%s** (%s) is invalid and won't run: %v.", r.Name, link, r.Err)})
	if err != nil {
		s.log().Error("post an invalid routine", "routine", r.Name, "err", err)
	}
	s.record(ctx, api.Entry{Kind: api.EntryInvalid, Routine: r.Name, Text: r.Err.Error(), Turn: turn.ID})
	if err := s.Store.SetReported(ctx, r.Name, r.Hash); err != nil {
		s.log().Error("record a report", "routine", r.Name, "err", err)
	}
}

// Routines returns the routines, by name, for activity.routines.
func (s *Scheduler) Routines(ctx context.Context) ([]api.Routine, error) {
	s.mu.Lock()
	rs := s.sorted()
	s.mu.Unlock()
	now := s.now()
	out := make([]api.Routine, 0, len(rs))
	for _, r := range rs {
		a := api.Routine{Name: r.Name, Path: r.Path, URL: r.URL, Valid: r.Valid(), Schedule: r.Schedule,
			Timezone: r.Zone, Output: r.Output, Enabled: r.Enabled}
		if r.Err != nil {
			a.Error = r.Err.Error()
		}
		if next, ok := r.Next(now); ok {
			a.Next = next.UTC().Format(time.RFC3339)
		}
		st, err := s.Store.SeeRoutine(ctx, r.Name, s.now())
		if err != nil {
			return nil, err
		}
		if !st.LastScheduled.IsZero() {
			a.Last, a.LastOK = st.LastScheduled.UTC().Format(time.RFC3339), st.LastOK
		}
		out = append(out, a)
	}
	return out, nil
}
