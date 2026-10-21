// Package routine is Porter's scheduler: it reads routines, scheduled
// prompts the owner keeps as notes in Porter/Routines/, runs each when it
// is due with the unattended tools, writes its output note, and records
// Activity (docs/specs/routines.md).
package routine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Folder is where routines live: notes directly in it are routines.
const Folder = "Porter/Routines/"

// MaxPrompt is the longest a routine's body may be, in bytes.
const MaxPrompt = 16384

// Routine is one routine note, parsed. Err is why it is invalid; an
// invalid routine never runs.
type Routine struct {
	// Name is the file name without .md.
	Name string
	// Path is the note's path in the vault.
	Path string
	// URL is a lodge address of the note, when notes gave one.
	URL string
	// Hash is the version of the note this was parsed from.
	Hash string

	Schedule string
	Zone     string
	Output   string
	Thinking bool
	Model    string
	Enabled  bool
	Prompt   string

	sched Schedule
	loc   *time.Location
	Err   error
}

// front is the frontmatter a routine may have; any other key is an error.
type front struct {
	Schedule *string `yaml:"schedule"`
	Timezone *string `yaml:"timezone"`
	Output   *string `yaml:"output"`
	Thinking *bool   `yaml:"thinking"`
	Model    *string `yaml:"model"`
	Enabled  *bool   `yaml:"enabled"`
}

var (
	aliasName   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	placeholder = regexp.MustCompile(`\{\{[^}]*\}\}`)
)

// Parse reads the routine note at path from its content. zone and model
// are the defaults for a routine that names neither.
func Parse(notePath, content string, zone *time.Location, model string) Routine {
	r := Routine{Path: notePath, Name: strings.TrimSuffix(path.Base(notePath), ".md"), Enabled: true,
		Model: model, loc: zone, Zone: zone.String()}
	r.Err = r.parse(content)
	return r
}

func (r *Routine) parse(content string) error {
	head, body, err := splitFrontmatter(content)
	if err != nil {
		return err
	}
	var f front
	dec := yaml.NewDecoder(strings.NewReader(head))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("the frontmatter: %v", yamlError(err))
	}
	if f.Schedule == nil || strings.TrimSpace(*f.Schedule) == "" {
		return errors.New("schedule is missing")
	}
	r.Schedule = strings.TrimSpace(*f.Schedule)
	if r.sched, err = ParseSchedule(r.Schedule); err != nil {
		return err
	}
	if f.Timezone != nil {
		z := strings.TrimSpace(*f.Timezone)
		loc, err := time.LoadLocation(z)
		if err != nil || z == "" || z == "Local" {
			return fmt.Errorf("timezone %q is not an IANA zone", z)
		}
		r.loc, r.Zone = loc, z
	}
	if f.Output != nil {
		if err := checkOutput(*f.Output); err != nil {
			return err
		}
		r.Output = *f.Output
	}
	if f.Thinking != nil {
		r.Thinking = *f.Thinking
	}
	if f.Model != nil {
		if !aliasName.MatchString(*f.Model) {
			return fmt.Errorf("model %q is not an inference alias", *f.Model)
		}
		r.Model = *f.Model
	}
	if f.Enabled != nil {
		r.Enabled = *f.Enabled
	}
	body = strings.TrimLeft(body, "\r\n")
	switch {
	case strings.TrimSpace(body) == "":
		return errors.New("the prompt, the note's body, is empty")
	case len(body) > MaxPrompt:
		return fmt.Errorf("the prompt is %d bytes, over %d", len(body), MaxPrompt)
	}
	r.Prompt = body
	return nil
}

// splitFrontmatter returns a note's YAML frontmatter and its body. A
// routine must have frontmatter, for its schedule.
func splitFrontmatter(content string) (string, string, error) {
	c := strings.TrimPrefix(content, "\uFEFF")
	first, rest, ok := strings.Cut(c, "\n")
	if !ok || strings.TrimRight(first, "\r") != "---" {
		return "", "", errors.New("it has no frontmatter; a routine starts with --- and a schedule")
	}
	for i := 0; i < len(rest); {
		line, _, _ := strings.Cut(rest[i:], "\n")
		if strings.TrimRight(line, "\r") == "---" {
			end := min(i+len(line)+1, len(rest))
			return rest[:i], rest[end:], nil
		}
		i += len(line) + 1
	}
	return "", "", errors.New("its frontmatter has no closing ---")
}

// yamlError trims yaml's prefix from an error, and names an unknown field.
func yamlError(err error) string {
	s := strings.TrimPrefix(err.Error(), "yaml: ")
	if i := strings.Index(s, "field "); i >= 0 && strings.Contains(s, "not found in type") {
		name, _, _ := strings.Cut(s[i+len("field "):], " ")
		return fmt.Sprintf("%q is not a routine field (schedule, timezone, output, thinking, model, enabled)", name)
	}
	return s
}

// checkOutput applies the rules for an output path template.
func checkOutput(o string) error {
	bad := func(why string) error { return fmt.Errorf("output %q %s", o, why) }
	switch {
	case !strings.HasSuffix(o, ".md"):
		return bad("does not end in .md")
	case !strings.HasPrefix(o, "Porter/"):
		return bad("is not under Porter/")
	case strings.HasPrefix(strings.ToLower(o), strings.ToLower(Folder)):
		return bad("is under Porter/Routines/, which Porter cannot write")
	case strings.Contains(o, "\\") || bytes.ContainsAny([]byte(o), "\x00\r\n"):
		return bad("has a character a path may not")
	}
	for _, seg := range strings.Split(o, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return bad("has an empty, . or .. segment")
		}
	}
	for _, p := range placeholder.FindAllString(o, -1) {
		switch p {
		case "{{date}}", "{{week}}", "{{weekday}}":
		default:
			return bad("has an unknown placeholder " + p + " (use {{date}}, {{week}} or {{weekday}})")
		}
	}
	return nil
}

// Valid says whether the routine may run.
func (r Routine) Valid() bool { return r.Err == nil }

// Location is the zone the routine's schedule is in.
func (r Routine) Location() *time.Location { return r.loc }

// Due says whether the routine is scheduled at t's minute.
func (r Routine) Due(t time.Time) bool { return r.Valid() && r.sched.Matches(t.In(r.loc)) }

// Last returns the routine's latest scheduled minute in (after, upto].
func (r Routine) Last(after, upto time.Time) (time.Time, bool) {
	if !r.Valid() {
		return time.Time{}, false
	}
	return r.sched.Last(after, upto, r.loc)
}

// Next returns the routine's first scheduled minute after t.
func (r Routine) Next(t time.Time) (time.Time, bool) {
	if !r.Valid() || !r.Enabled {
		return time.Time{}, false
	}
	return r.sched.Next(t, r.loc)
}

// OutputPath is the output note's path for a run scheduled at t, or ""
// when the routine has no output.
func (r Routine) OutputPath(t time.Time) string {
	if r.Output == "" {
		return ""
	}
	lt := t.In(r.loc)
	y, w := lt.ISOWeek()
	return strings.NewReplacer(
		"{{date}}", lt.Format("2006-01-02"),
		"{{week}}", fmt.Sprintf("%04d-W%02d", y, w),
		"{{weekday}}", lt.Weekday().String(),
	).Replace(r.Output)
}
