package workflowcheck

import (
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// skipped reports whether the repository walk leaves directory name out:
// version control, dependencies, build output, and each name in extra.
func skipped(name string, extra map[string]bool) bool {
	switch name {
	case ".git", "node_modules", "build", "dist", "target":
		return true
	}
	return extra[name]
}

// repoFiles returns every file of the repository as a path from its root with
// forward slashes. The directories skipped, and each of extra, are left out.
func repoFiles(t *testing.T, extra ...string) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	skip := make(map[string]bool, len(extra))
	for _, name := range extra {
		skip[name] = true
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipped(d.Name(), skip) {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestInstructionAliases guards row B1 of frostyard/core
// docs/specs/repository-baseline.md: AGENTS.md is the one instruction file,
// and every other agent entry point is a relative symlink to it.
func TestInstructionAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows checkout has no symlinks")
	}
	agents := filepath.Join("..", "..", "AGENTS.md")
	fi, err := os.Lstat(agents)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() {
		t.Errorf("AGENTS.md is not a regular file (mode %s); it is the one instruction file", fi.Mode())
	}
	want, err := filepath.EvalSymlinks(agents)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"CLAUDE.md",
		"GEMINI.md",
		".cursorrules",
		filepath.Join(".github", "copilot-instructions.md"),
	} {
		if got, ok := relativeSymlink(t, name); ok && got != want {
			t.Errorf("%s resolves to %q, want %q", name, got, want)
		}
	}

	name := filepath.Join(".claude", "skills")
	got, ok := relativeSymlink(t, name)
	wantSkills, err := filepath.EvalSymlinks(filepath.Join("..", "..", ".agents", "skills"))
	if err != nil {
		t.Fatalf(".agents/skills: %v", err)
	}
	if ok && got != wantSkills {
		t.Errorf("%s resolves to %q, want %q", name, got, wantSkills)
	}
	if fi, err := os.Stat(filepath.Join("..", "..", name)); err != nil {
		t.Errorf("%s: %v", name, err)
	} else if !fi.IsDir() {
		t.Errorf("%s resolves to a file, want the directory .agents/skills", name)
	}
}

// relativeSymlink reports whether name is a symlink whose target is relative,
// and returns the path it resolves to.
func relativeSymlink(t *testing.T, name string) (string, bool) {
	t.Helper()
	link := filepath.Join("..", "..", name)
	fi, err := os.Lstat(link)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return "", false
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is not a symlink", name)
		return "", false
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return "", false
	}
	if filepath.IsAbs(target) {
		t.Errorf("%s links to the absolute path %q; it must be relative", name, target)
		return "", false
	}
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return "", false
	}
	return got, true
}

// correctionsMember names the 5 members a line of .memory/corrections.jsonl
// carries (row B2).
var correctionsMember = []string{"date", "scope", "correction", "evidence", "promoted_to"}

// datePattern is the form of a correction's date.
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// correctionProblems returns the problems of 1 line of
// .memory/corrections.jsonl, after frostyard/core scripts/check-corrections.mjs
// (row B2); no problem means a valid line. promoted_to may be empty, the way an
// unpromoted correction has it.
func correctionProblems(line string) []string {
	if strings.TrimSpace(line) == "" {
		return []string{"blank line"}
	}
	var value any
	if err := json.Unmarshal([]byte(line), &value); err != nil {
		return []string{fmt.Sprintf("invalid JSON: %v", err)}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return []string{"not a plain object"}
	}

	var problems, missing, unknown []string
	for _, name := range correctionsMember {
		if _, ok := obj[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range obj {
		if !slices.Contains(correctionsMember, name) {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	if len(missing) > 0 {
		problems = append(problems, "missing key(s): "+strings.Join(missing, ", "))
	}
	if len(unknown) > 0 {
		problems = append(problems, "unknown key(s): "+strings.Join(unknown, ", "))
	}
	for _, name := range correctionsMember {
		v, ok := obj[name]
		if !ok {
			continue
		}
		if _, ok := v.(string); !ok {
			problems = append(problems, "value for "+name+" is not a string")
		}
	}
	if s, ok := obj["date"].(string); ok {
		if !datePattern.MatchString(s) {
			problems = append(problems, "invalid date: "+s)
		} else if _, err := time.Parse("2006-01-02", s); err != nil {
			problems = append(problems, "invalid date: "+s)
		}
	}
	for _, name := range []string{"scope", "correction", "evidence"} {
		if s, ok := obj[name].(string); ok && strings.TrimSpace(s) == "" {
			problems = append(problems, name+" is empty")
		}
	}
	return problems
}

// correctionsLines returns the lines of .memory/corrections.jsonl that hold
// content: an empty file gives no line, and the last newline does not give
// one. A blank line inside the file stays, and it is a problem.
func correctionsLines(data string) []string {
	if data == "" {
		return nil
	}
	lines := strings.Split(data, "\n")
	if n := len(lines); lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// TestCorrectionsInbox guards row B2 of frostyard/core
// docs/specs/repository-baseline.md: .memory/corrections.jsonl holds one JSON
// object per line, with exactly the 5 fields of a correction. An empty file
// is valid too, as core's scripts/check-corrections.mjs accepts it.
func TestCorrectionsInbox(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".memory", "corrections.jsonl"))
	if err != nil {
		t.Fatalf(".memory/corrections.jsonl: %v", err)
	}
	lines := correctionsLines(string(data))
	for i, line := range lines {
		for _, problem := range correctionProblems(line) {
			t.Errorf("line %d: %s", i+1, problem)
		}
	}
	t.Logf("%d corrections lines", len(lines))
}

// TestCorrectionsLines checks that empty content gives no line, and so no
// problem, and that a blank line inside the content is a problem.
func TestCorrectionsLines(t *testing.T) {
	const valid = `{"date": "2026-11-05", "scope": "socket", "correction": "c", "evidence": "e", "promoted_to": ""}`
	if lines := correctionsLines(""); len(lines) != 0 {
		t.Errorf("correctionsLines of empty content = %q, want no line", lines)
	}
	for i, line := range correctionsLines("") {
		if problems := correctionProblems(line); len(problems) > 0 {
			t.Errorf("line %d of empty content: %v, want no problem", i+1, problems)
		}
	}
	if lines := correctionsLines(valid); !slices.Equal(lines, []string{valid}) {
		t.Errorf("correctionsLines of 1 line without a newline = %q, want 1 line", lines)
	}
	lines := correctionsLines(valid + "\n\n")
	if len(lines) != 2 {
		t.Fatalf("correctionsLines of 1 line and a blank line = %q, want 2 lines", lines)
	}
	if problems := correctionProblems(lines[1]); len(problems) == 0 {
		t.Errorf("the blank line inside the content gives no problem, want at least 1")
	}
}

// TestCorrectionProblems checks the line rules of row B2 on the cases
// frostyard/core scripts/check-corrections.mjs names.
func TestCorrectionProblems(t *testing.T) {
	const valid = `{"date": "2026-11-05", "scope": "socket", "correction": "c", "evidence": "e", "promoted_to": ""}`
	for _, tc := range []struct {
		name  string
		line  string
		valid bool
	}{
		{"unpromoted", valid, true},
		{"promoted", `{"date": "2026-11-05", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": "a_test.go"}`, true},
		{"blank scope", `{"date": "2026-11-05", "scope": "  ", "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"month 13", `{"date": "2026-13-99", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"february 30", `{"date": "2026-02-30", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"number value", `{"date": "2026-11-05", "scope": 1, "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"missing member", `{"date": "2026-11-05", "scope": "s", "correction": "c", "evidence": "e"}`, false},
		{"unknown member", `{"date": "2026-11-05", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": "", "extra": "x"}`, false},
		{"duplicate member", `{"date": "2026-11-05", "date": "2026-11-05", "scope": "s", "correction": "c", "evidence": "e", "promoted_to": ""}`, false},
		{"not an object", `["2026-11-05"]`, false},
		{"blank line", "  ", false},
	} {
		problems := correctionProblems(tc.line)
		switch {
		case tc.valid && len(problems) > 0:
			t.Errorf("%s: correctionProblems(%s) = %v, want none", tc.name, tc.line, problems)
		case !tc.valid && len(problems) == 0:
			t.Errorf("%s: correctionProblems(%s) = none, want at least 1", tc.name, tc.line)
		}
	}
}

// riskPath is 1 entry of .github/risk-paths.json.
type riskPath struct {
	Glob  string `json:"glob"`
	Level string `json:"level"`
	Why   string `json:"why"`
}

// riskPathFile is the whole of .github/risk-paths.json.
type riskPathFile struct {
	SchemaVersion int        `json:"schema_version"`
	DefaultLevel  string     `json:"default_level"`
	Paths         []riskPath `json:"paths"`
}

// riskLevels are the levels of frostyard/core docs/specs/risk-paths.md; L1 is
// the strictest level.
var riskLevels = []string{"L1", "L2", "L3", "L4"}

// requiredRiskPaths are the paths that a list must match at L1 or L2
// (frostyard/core docs/specs/risk-paths.md, rule C).
var requiredRiskPaths = []string{
	".github/risk-paths.json",
	".github/workflows/risk-paths.yml",
}

// riskCategories are the categories of frostyard/core docs/specs/risk-paths.md
// (section B); each why starts with 1 of them and a colon.
var riskCategories = []string{
	"boot chain",
	"signing and keys",
	"update verification",
	"auth",
	"publication",
	"secrets and credentials",
	"rulesets and settings",
	"destructive operations",
	"governance",
}

// whyCategory reports whether why starts with 1 of riskCategories and a colon.
func whyCategory(why string) bool {
	category, _, ok := strings.Cut(why, ":")
	return ok && slices.Contains(riskCategories, category)
}

// levelRank returns the rank of level, where a lower rank is stricter; a level
// outside the 4 values is the loosest rank.
func levelRank(level string) int {
	if i := slices.Index(riskLevels, level); i >= 0 {
		return i
	}
	return len(riskLevels)
}

// strictestLevel returns the strictest level of the globs of entries that
// match p, and whether 1 of them matches.
func strictestLevel(entries []riskPath, p string) (level string, ok bool) {
	for _, e := range entries {
		if !matchGlob(e.Glob, p) {
			continue
		}
		if !ok || levelRank(e.Level) < levelRank(level) {
			level, ok = e.Level, true
		}
	}
	return level, ok
}

// riskPathProblems returns the reasons cfg is not a version 2 list of
// frostyard/core docs/specs/risk-paths.md; no reason means a valid list.
func riskPathProblems(cfg riskPathFile) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if cfg.SchemaVersion != 2 {
		add("schema_version = %d, want 2", cfg.SchemaVersion)
	}
	if !slices.Contains(riskLevels, cfg.DefaultLevel) {
		add("default_level = %q, want 1 of %q", cfg.DefaultLevel, riskLevels)
	}
	if len(cfg.Paths) == 0 {
		add("the list has no path")
	}
	seen := make(map[string]bool, len(cfg.Paths))
	for i, p := range cfg.Paths {
		for _, problem := range validGlob(p.Glob) {
			add("entry %d: glob %q: %s", i, p.Glob, problem)
		}
		if seen[p.Glob] {
			add("entry %d: glob %q appears twice", i, p.Glob)
		}
		seen[p.Glob] = true
		if !slices.Contains(riskLevels, p.Level) {
			add("entry %d: glob %q has the level %q, want 1 of %q", i, p.Glob, p.Level, riskLevels)
		}
		if strings.TrimSpace(p.Why) == "" {
			add("entry %d: glob %q has no why", i, p.Glob)
		}
		if n := utf8.RuneCountInString(p.Why); n > 200 {
			add("entry %d: glob %q has a why of %d characters, want 200 at most", i, p.Glob, n)
		}
		if levelRank(p.Level) <= levelRank("L2") && !whyCategory(p.Why) {
			add("entry %d: glob %q is at %s and its why %q must start with 1 of the categories %q and a colon", i, p.Glob, p.Level, p.Why, riskCategories)
		}
	}
	for _, required := range requiredRiskPaths {
		matched := false
		for _, p := range cfg.Paths {
			if !matchGlob(p.Glob, required) {
				continue
			}
			matched = true
			if levelRank(p.Level) > levelRank("L2") {
				add("rule C: the glob %q matches %s and is at %s, want L1 or L2", p.Glob, required, p.Level)
			}
		}
		if !matched {
			add("rule C: no glob matches %s", required)
		}
	}
	return problems
}

// governedPaths are the release, secrets, auth and governance paths of this
// repository; .github/risk-paths.json must list each of them.
var governedPaths = []string{
	".github/risk-paths.json",
	".github/workflows/risk-paths.yml",
	".github/workflows/release.yml",
	".github/workflows/ci.yml",
	"Makefile",
	"mise.toml",
	"mise.lock",
	".svu.yml",
	"internal/workflowcheck/baseline_test.go",
	"internal/workflowcheck/workflows_test.go",
	"internal/packagecheck/package_test.go",
	"internal/packagecheck/debcheck_test.go",
	"packaging/nfpm.yaml",
	"packaging/deb-check.py",
	"packaging/lodge/app.json",
	"packaging/lodge/frostporter.service",
	"packaging/lodge/frostporter.socket",
	"packaging/lodge/stage.sh",
	"packaging/lodge/sysusers.d-frostporter.conf",
	"internal/config/config.go",
	"cmd/porterd/main.go",
	"cmd/porterd/main_test.go",
	"api/api.go",
	"api/rpc.go",
	"api/thread.go",
	"api/activity.go",
	"internal/rpcserver/server.go",
	"internal/tools/classify.go",
	"internal/tools/registry.go",
	"internal/tools/tools.go",
	"internal/agent/agent.go",
	"internal/agent/run.go",
	"internal/agent/own.go",
	"internal/routine/parse.go",
	"internal/thread/store.go",
}

// readRiskPaths reads and parses .github/risk-paths.json strictly: an unknown
// member fails the test, so a new field of the file needs this test.
func readRiskPaths(t *testing.T) riskPathFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "risk-paths.json"))
	if err != nil {
		t.Fatalf(".github/risk-paths.json: %v", err)
	}
	var cfg riskPathFile
	if err := json.Unmarshal(data, &cfg, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf(".github/risk-paths.json: %v", err)
	}
	return cfg
}

// riskMarkers are the lines of Go source that mark a file as a risk path: a
// method's callers, the unattended tools of routines, a file's mode, the
// private socket, and the thread database's schema.
var riskMarkers = []string{
	"[]CallerKind{",
	".Unattended()",
	".Chmod(",
	"0o600",
	"0o700",
	"ALTER TABLE",
	"CREATE TABLE",
	"jsonrpc.Listen",
}

// riskMarker returns the first marker of riskMarkers that src holds, in the
// order of the list; no marker means plain source.
func riskMarker(src string) (string, bool) {
	for _, marker := range riskMarkers {
		if strings.Contains(src, marker) {
			return marker, true
		}
	}
	return "", false
}

// TestRiskPaths guards row B6 of frostyard/core
// docs/specs/repository-baseline.md: .github/risk-paths.json is a valid
// version 2 list, it lists every governed path of the repository at L1 or L2,
// and no glob is stale.
func TestRiskPaths(t *testing.T) {
	cfg := readRiskPaths(t)
	for _, problem := range riskPathProblems(cfg) {
		t.Errorf(".github/risk-paths.json: %s", problem)
	}

	want := append([]string{}, governedPaths...)
	workflows, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range workflows {
		want = append(want, path.Join(".github/workflows", filepath.Base(p)))
	}
	files := repoFiles(t)
	for _, p := range missingPaths(want, files) {
		t.Errorf("%s is missing; the repository must have it, and .github/risk-paths.json must list it", p)
	}
	for _, p := range want {
		level, ok := strictestLevel(cfg.Paths, p)
		switch {
		case !ok:
			t.Errorf("%s is a release, secrets, auth or governance path; .github/risk-paths.json must list it", p)
		case levelRank(level) > levelRank("L2"):
			t.Errorf("%s is a release, secrets, auth or governance path; .github/risk-paths.json lists it at %s, want L1 or L2", p, level)
		}
	}

	for _, g := range cfg.Paths {
		if !anyMatch(g.Glob, files) {
			t.Errorf("glob %q matches no file in the repository", g.Glob)
		}
	}
	t.Logf("%d globs; %d required paths listed; %d files searched", len(cfg.Paths), len(want), len(files))
}

// TestStrictestLevel checks the level of 1 path: the strictest level of the
// globs that match it.
func TestStrictestLevel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []riskPath
		path    string
		level   string
		ok      bool
	}{
		{"no match", []riskPath{{Glob: ".github/workflows/**", Level: "L2", Why: "governance: CI"}}, "internal/tools/tools.go", "", false},
		{"1 match", []riskPath{{Glob: ".github/workflows/**", Level: "L2", Why: "governance: CI"}}, ".github/workflows/ci.yml", "L2", true},
		{"2 matches", []riskPath{
			{Glob: ".github/workflows/**", Level: "L2", Why: "governance: CI"},
			{Glob: ".github/workflows/release.yml", Level: "L1", Why: "publication: the .deb release"},
		}, ".github/workflows/release.yml", "L1", true},
		{"a looser glob listed first", []riskPath{
			{Glob: "internal/**", Level: "L3", Why: "ordinary code"},
			{Glob: "internal/agent/**", Level: "L1", Why: "auth: the approval cards"},
		}, "internal/agent/run.go", "L1", true},
	} {
		level, ok := strictestLevel(tc.entries, tc.path)
		if ok != tc.ok || level != tc.level {
			t.Errorf("%s: strictestLevel(%q) = %q, %v, want %q, %v", tc.name, tc.path, level, ok, tc.level, tc.ok)
		}
	}
}

// TestRiskPathProblems checks the validation of a version 2 list of
// frostyard/core docs/specs/risk-paths.md on the cases a repository's list can
// hold.
func TestRiskPathProblems(t *testing.T) {
	list := riskPath{Glob: ".github/risk-paths.json", Level: "L2", Why: "governance: the list"}
	caller := riskPath{Glob: ".github/workflows/risk-paths.yml", Level: "L2", Why: "governance: the caller"}
	valid := func(entries ...riskPath) riskPathFile {
		return riskPathFile{SchemaVersion: 2, DefaultLevel: "L3", Paths: append([]riskPath{list, caller}, entries...)}
	}
	for _, tc := range []struct {
		name string
		cfg  riskPathFile
		want string // a substring of the 1 problem the case must give; "" for a valid list
	}{
		{"valid v2 list", valid(), ""},
		{"an L3 entry with a free-text why", valid(riskPath{Glob: "docs/how-to/**", Level: "L3", Why: "how-to documentation"}), ""},
		{"an L4 entry with a free-text why", valid(riskPath{Glob: "docs/how-to/**", Level: "L4", Why: "how-to documentation"}), ""},
		{"a bad level", valid(riskPath{Glob: "signing/**", Level: "L5", Why: "signing and keys"}),
			`has the level "L5"`},
		{"no default_level", riskPathFile{SchemaVersion: 2, Paths: []riskPath{list, caller}},
			`default_level = ""`},
		{"schema_version 1", riskPathFile{SchemaVersion: 1, DefaultLevel: "L3", Paths: []riskPath{list, caller}},
			"schema_version = 1, want 2"},
		{"an L1 entry with a free-text why", valid(riskPath{Glob: "internal/config/config.go", Level: "L1", Why: "the app tokens"}),
			"must start with 1 of the categories"},
		{"an L3 entry with no why", valid(riskPath{Glob: "Makefile", Level: "L3", Why: ""}),
			`glob "Makefile" has no why`},
		{"an L3 glob for the list", riskPathFile{SchemaVersion: 2, DefaultLevel: "L3", Paths: []riskPath{
			{Glob: ".github/risk-paths.json", Level: "L3", Why: "governance: the list"},
			caller,
		}}, `matches .github/risk-paths.json and is at L3, want L1 or L2`},
		{"no glob for the caller", riskPathFile{SchemaVersion: 2, DefaultLevel: "L3", Paths: []riskPath{list}},
			"no glob matches .github/workflows/risk-paths.yml"},
	} {
		problems := riskPathProblems(tc.cfg)
		if tc.want == "" {
			if len(problems) > 0 {
				t.Errorf("%s: riskPathProblems = %v, want none", tc.name, problems)
			}
			continue
		}
		if len(problems) != 1 {
			t.Errorf("%s: riskPathProblems = %v, want 1 problem containing %q", tc.name, problems, tc.want)
			continue
		}
		if !strings.Contains(problems[0], tc.want) {
			t.Errorf("%s: riskPathProblems = %q, want it to contain %q", tc.name, problems[0], tc.want)
		}
	}
}

// TestRiskMarkers guards the risk list against the next omission: a Go file
// that admits a caller to a method, holds the unattended tool set, sets a
// file's mode, opens the private socket or holds the thread database's schema
// is an auth, credential or schema check, whatever package it is in. A glob
// that names files misses such a file when a function moves to a new one.
func TestRiskMarkers(t *testing.T) {
	cfg := readRiskPaths(t)
	marked := 0
	for _, f := range repoFiles(t) {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(f)))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		marker, ok := riskMarker(string(data))
		if !ok {
			continue
		}
		marked++
		if level, ok := strictestLevel(cfg.Paths, f); !ok || levelRank(level) > levelRank("L2") {
			t.Errorf("%s has %s (an auth, credential or schema check); .github/risk-paths.json must list it at L1 or L2", f, marker)
		}
	}
	t.Logf("%d globs; %d Go files with a marker", len(cfg.Paths), marked)
}

// TestRiskMarker checks the marker list on source that holds each marker, and
// on plain source.
func TestRiskMarker(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"method admission", "callers := []CallerKind{CallerLocal, CallerSession}", "[]CallerKind{"},
		{"unattended tools", "if !unattended || t.Kind.Unattended() {", ".Unattended()"},
		{"file mode by Chmod", "if err := tmp.Chmod(0o644); err != nil {", ".Chmod("},
		{"token file mode", "os.WriteFile(path, data, 0o600)", "0o600"},
		{"state directory mode", "os.MkdirAll(state, 0o700)", "0o700"},
		{"column migration", "ALTER TABLE routines ADD COLUMN last_success TEXT", "ALTER TABLE"},
		{"schema", "CREATE TABLE IF NOT EXISTS turns (", "CREATE TABLE"},
		{"private socket", "private, err := jsonrpc.Listen(socket, log)", "jsonrpc.Listen"},
		{"first marker in the list order", "if err := tmp.Chmod(0o600); err != nil {", ".Chmod("},
		{"plain source", "package api\n\nfunc Serve() {}\n", ""},
	} {
		got, ok := riskMarker(tc.src)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: riskMarker(%q) = %q, %v, want %q", tc.name, tc.src, got, ok, tc.want)
		}
	}
}

// missingPaths returns each of required that is not in files, in order. A
// required path must exist: a glob such as .github/workflows/** still
// matches the name of a deleted caller.
func missingPaths(required, files []string) []string {
	var missing []string
	for _, p := range required {
		if !slices.Contains(files, p) {
			missing = append(missing, p)
		}
	}
	return missing
}

// TestMissingPaths checks that a required path that is not in the
// repository is missing, also when a wide glob still matches its name.
func TestMissingPaths(t *testing.T) {
	files := []string{".github/risk-paths.json", ".github/workflows/ci.yml"}
	required := []string{".github/risk-paths.json", ".github/workflows/risk-paths.yml"}
	if !matchAny([]string{".github/workflows/**"}, ".github/workflows/risk-paths.yml") {
		t.Fatal("the wide glob does not match the caller's name")
	}
	got := missingPaths(required, files)
	if want := []string{".github/workflows/risk-paths.yml"}; !slices.Equal(got, want) {
		t.Errorf("missingPaths(%q, %q) = %q, want %q", required, files, got, want)
	}
	if got := missingPaths(files, files); len(got) != 0 {
		t.Errorf("missingPaths of present paths = %q, want none", got)
	}
}

// matchAny reports whether p matches 1 of globs.
func matchAny(globs []string, p string) bool {
	for _, g := range globs {
		if matchGlob(g, p) {
			return true
		}
	}
	return false
}

// anyMatch reports whether glob matches 1 of paths.
func anyMatch(glob string, paths []string) bool {
	for _, p := range paths {
		if matchGlob(glob, p) {
			return true
		}
	}
	return false
}

// maxBraceExpansions is the most globs 1 brace group set may give
// (frostyard/core scripts/lib/risk-paths.mjs).
const maxBraceExpansions = 256

// matchGlob reports whether glob matches the whole slash-separated path
// (frostyard/core docs/specs/risk-paths.md): {a,b} is 1 of the alternatives,
// ** as a whole segment is 0 or more whole segments, * and ? never cross a
// slash, and every other character is literal. An empty path matches nothing,
// and a glob that cannot be expanded matches nothing.
func matchGlob(glob, p string) bool {
	if p == "" {
		return false
	}
	expansions, errs := expandBraces(glob)
	if len(errs) > 0 {
		return false
	}
	pathSegments := strings.Split(p, "/")
	for _, expansion := range expansions {
		if matchSegments(strings.Split(expansion, "/"), pathSegments) {
			return true
		}
	}
	return false
}

// expandBraces returns 1 expansion of glob for each combination of its brace
// groups, and the reasons glob cannot be expanded; a glob with a reason expands
// to nothing. Groups do not nest.
func expandBraces(glob string) (expansions []string, errs []string) {
	var parts [][]string
	literal := ""
	for i := 0; i < len(glob); {
		switch glob[i] {
		case '}':
			errs = append(errs, "unmatched '}'")
			i++
		case '{':
			rest := glob[i+1:]
			close, nextOpen := strings.IndexByte(rest, '}'), strings.IndexByte(rest, '{')
			if close < 0 {
				return nil, append(errs, "unmatched '{'")
			}
			if nextOpen >= 0 && nextOpen < close {
				return nil, append(errs, "nested braces are not supported")
			}
			alternatives := strings.Split(rest[:close], ",")
			if len(alternatives) < 2 {
				errs = append(errs, "a brace group needs at least 2 alternatives")
			}
			if slices.Contains(alternatives, "") {
				errs = append(errs, "a brace group has an empty alternative")
			}
			parts = append(parts, []string{literal}, alternatives)
			literal = ""
			i += close + 2
		default:
			literal += glob[i : i+1]
			i++
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	parts = append(parts, []string{literal})

	expansions = []string{""}
	for _, alternatives := range parts {
		next := make([]string, 0, len(expansions)*len(alternatives))
		for _, prefix := range expansions {
			for _, alternative := range alternatives {
				next = append(next, prefix+alternative)
			}
		}
		if len(next) > maxBraceExpansions {
			return nil, []string{fmt.Sprintf("brace expansion gives more than %d globs", maxBraceExpansions)}
		}
		expansions = next
	}
	return expansions, nil
}

// matchSegments reports whether the path segments match the glob segments,
// where a ** segment is 0 or more whole segments.
func matchSegments(globSegments, pathSegments []string) bool {
	patterns := make([]*regexp.Regexp, len(globSegments))
	for i, segment := range globSegments {
		if segment != "**" {
			patterns[i] = segmentPattern(segment)
		}
	}
	memo := make(map[[2]int]bool, len(globSegments)*len(pathSegments)+1)
	var visit func(g, p int) bool
	visit = func(g, p int) bool {
		if match, ok := memo[[2]int{g, p}]; ok {
			return match
		}
		var match bool
		switch {
		case g == len(patterns):
			match = p == len(pathSegments)
		case patterns[g] == nil:
			match = visit(g+1, p) || (p < len(pathSegments) && visit(g, p+1))
		default:
			match = p < len(pathSegments) && patterns[g].MatchString(pathSegments[p]) && visit(g+1, p+1)
		}
		memo[[2]int{g, p}] = match
		return match
	}
	return visit(0, 0)
}

// segmentPattern compiles 1 glob segment: * is any run of characters within the
// segment, ? is 1 such character, and every other character is literal.
func segmentPattern(segment string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, c := range segment {
		switch c {
		case '*':
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// validGlob returns the reasons glob is not a path glob of frostyard/core
// docs/specs/risk-paths.md; no reason means a valid glob.
func validGlob(glob string) []string {
	if glob == "" {
		return []string{"glob is empty"}
	}
	var errs []string
	for _, c := range []string{"[", "]", "!", `\`} {
		if strings.Contains(glob, c) {
			errs = append(errs, fmt.Sprintf("%q is not supported", c))
		}
	}
	expansions, braceErrs := expandBraces(glob)
	errs = append(errs, braceErrs...)
	for _, expansion := range expansions {
		if strings.HasPrefix(expansion, "/") {
			errs = append(errs, "a glob must not start with '/'")
		}
		if strings.HasPrefix(expansion, "./") {
			errs = append(errs, "a glob must not start with './'")
		}
		for _, segment := range strings.Split(expansion, "/") {
			switch {
			case segment == "":
				errs = append(errs, "a glob must not have an empty segment")
			case segment == "..":
				errs = append(errs, "a glob must not have a '..' segment")
			case strings.Contains(segment, "**") && segment != "**":
				errs = append(errs, "'**' must be a whole segment")
			}
		}
	}
	return unique(errs)
}

// unique returns list without a repeated entry, in the order of 1st appearance.
func unique(list []string) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// TestValidGlob checks the glob validation of frostyard/core
// docs/specs/risk-paths.md on the cases a repository's list can hold.
func TestValidGlob(t *testing.T) {
	for _, glob := range []string{"a/**", "{a,b}/c", "**", "internal/tools/{classify,registry,tools}.go"} {
		if problems := validGlob(glob); len(problems) > 0 {
			t.Errorf("validGlob(%q) = %v, want no problem", glob, problems)
		}
	}
	for _, glob := range []string{
		"internal/tools/{classify,}",
		"{a}",
		"{a,{b,c}}",
		"a}",
		"{a,b",
		"[ab]",
		"!a",
		`a\b`,
		"a**",
		"a/**b",
		"/a",
		"./a",
		"a//b",
		"a/../b",
		strings.Repeat("{a,b}", 9),
	} {
		if problems := validGlob(glob); len(problems) == 0 {
			t.Errorf("validGlob(%q) = no problem, want at least 1", glob)
		}
	}
}

// TestWhyCategory checks that a why names 1 category of frostyard/core
// docs/specs/risk-paths.md, before a colon.
func TestWhyCategory(t *testing.T) {
	for why, want := range map[string]bool{
		"auth: the app token per app":                true,
		"destructive operations: a schema migration": true,
		"security: the state directory's mode":       false,
		"auth the app token per app":                 false,
		"Auth: the app token per app":                false,
		"governance":                                 false,
		" governance: the list":                      false,
	} {
		if got := whyCategory(why); got != want {
			t.Errorf("whyCategory(%q) = %v, want %v", why, got, want)
		}
	}
}

// TestMatchGlob checks the glob rules of frostyard/core
// docs/specs/risk-paths.md on its own cases and the ones this repository's
// list holds.
func TestMatchGlob(t *testing.T) {
	for _, tc := range []struct {
		glob string
		path string
		want bool
	}{
		{"**/x", "x", true},
		{"**/x", "a/x", true},
		{"**/x", "a/b/x", true},
		{"**/x", "a/x/y", false},
		{"**", "x", true},
		{"**", "a/b", true},
		{"**", ".github/x.yml", true},
		{"x*", "x", true},
		{"x*", "xy", true},
		{"x*", "a/x", false},
		{"x*", "a/xy", false},
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/.md", true},
		{"docs/*.md", "docs/a/b.md", false},
		{"docs/*.md", "src/a.md", false},
		{"shared/**", "shared/a", true},
		{"shared/**", "shared/a/b", true},
		{"shared/**", "other/a", false},
		{"a/**/x", "a/x", true},
		{"a/**/x", "a/b/x", true},
		{"a/*", "a/b", true},
		{"a/*", "a/b/c", false},
		{"**", "", false},
		{"*.md", "", false},
		{".github/{risk-paths.json,settings.yml}", ".github/risk-paths.json", true},
		{".github/{risk-paths.json,settings.yml}", ".github/settings.yml", true},
		{".github/{risk-paths.json,settings.yml}", ".github/workflows/ci.yml", false},
		{".github/workflows/**", ".github/workflows/ci.yml", true},
		{".github/workflows/**", ".github/workflows/risk-paths.yml", true},
		{".github/workflows/**", ".github/risk-paths.json", false},
		{"packaging/lodge/**", "packaging/lodge/stage.sh", true},
		{"packaging/lodge/**", "packaging/lodge/sysusers.d-frostporter.conf", true},
		{"packaging/lodge/**", "packaging/nfpm.yaml", false},
		{"packaging/{nfpm.yaml,deb-check.py}", "packaging/deb-check.py", true},
		{"packaging/{nfpm.yaml,deb-check.py}", "packaging/lodge/deb-check.py", false},
		{"internal/{workflowcheck,packagecheck}/**", "internal/packagecheck/debcheck_test.go", true},
		{"internal/{workflowcheck,packagecheck}/**", "internal/workflowcheck/baseline_test.go", true},
		{"internal/{workflowcheck,packagecheck}/**", "internal/config/config.go", false},
		{"internal/tools/{classify,registry,tools}.go", "internal/tools/classify.go", true},
		{"internal/tools/{classify,registry,tools}.go", "internal/tools/tools.go", true},
		{"internal/tools/{classify,registry,tools}.go", "internal/tools/tools_test.go", false},
		{"internal/tools/{classify,registry,tools}.go", "internal/tools/classify_test.go", false},
		{"{api/api.go,api/rpc.go,api/thread.go,api/activity.go,internal/rpcserver/server.go}", "api/api.go", true},
		{"{api/api.go,api/rpc.go,api/thread.go,api/activity.go,internal/rpcserver/server.go}", "api/rpc.go", true},
		{"{api/api.go,api/rpc.go,api/thread.go,api/activity.go,internal/rpcserver/server.go}", "api/thread.go", true},
		{"{api/api.go,api/rpc.go,api/thread.go,api/activity.go,internal/rpcserver/server.go}", "api/activity.go", true},
		{"{api/api.go,api/rpc.go,api/thread.go,api/activity.go,internal/rpcserver/server.go}", "internal/rpcserver/server.go", true},
		{"{api/api.go,api/rpc.go,api/thread.go,api/activity.go,internal/rpcserver/server.go}", "api/api_test.go", false},
		{"{api/api.go,api/rpc.go,api/thread.go,api/activity.go,internal/rpcserver/server.go}", "api/other.go", false},
		{"internal/agent/{agent,run,own}.go", "internal/agent/agent.go", true},
		{"internal/agent/{agent,run,own}.go", "internal/agent/run.go", true},
		{"internal/agent/{agent,run,own}.go", "internal/agent/own.go", true},
		{"internal/agent/{agent,run,own}.go", "internal/agent/agent_test.go", false},
		{"internal/agent/{agent,run,own}.go", "internal/agent/prompt.go", false},
		{"{mise.toml,mise.lock}", "mise.toml", true},
		{"{mise.toml,mise.lock}", "mise.lock", true},
		{"{mise.toml,mise.lock}", "mise.tom", false},
		{"Makefile", "Makefile", true},
		{"Makefile", "app/Makefile", false},
		{".svu.yml", ".svu.yml", true},
		{".svu.yml", "docs/.svu.yml", false},
	} {
		if got := matchGlob(tc.glob, tc.path); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
}

// dependabotUpdate is 1 entry of the updates list of
// .github/dependabot.yml.
type dependabotUpdate struct {
	Ecosystem string `yaml:"package-ecosystem"`
	Directory string `yaml:"directory"`
}

// dependabotFile is the whole of .github/dependabot.yml.
type dependabotFile struct {
	Version int                `yaml:"version"`
	Updates []dependabotUpdate `yaml:"updates"`
}

// ecosystem names the Dependabot package-ecosystem of the manifest file
// base, if base is a manifest.
func ecosystem(base string) (string, bool) {
	switch base {
	case "go.mod":
		return "gomod", true
	case "package.json":
		return "npm", true
	case "Cargo.toml":
		return "cargo", true
	case "Dockerfile", "Containerfile":
		return "docker", true
	}
	return "", false
}

// normalizeDir drops the trailing slash of a Dependabot directory, and keeps
// the root as "/".
func normalizeDir(dir string) string {
	if dir == "/" {
		return dir
	}
	return strings.TrimSuffix(dir, "/")
}

// TestDependabotCoversManifests guards row B13 of frostyard/core
// docs/specs/repository-baseline.md: .github/dependabot.yml has 1 entry for
// each manifest of the repository, go.mod and web/package.json here, and the
// github-actions entry at "/".
func TestDependabotCoversManifests(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "dependabot.yml"))
	if err != nil {
		t.Fatalf(".github/dependabot.yml: %v", err)
	}
	var cfg dependabotFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf(".github/dependabot.yml: %v", err)
	}
	if cfg.Version != 2 {
		t.Errorf("version = %d, want 2", cfg.Version)
	}
	covered := make(map[[2]string]bool, len(cfg.Updates))
	actions := false
	for _, u := range cfg.Updates {
		dir := normalizeDir(u.Directory)
		covered[[2]string{u.Ecosystem, dir}] = true
		if u.Ecosystem == "github-actions" && dir == "/" {
			actions = true
		}
	}
	if !actions {
		t.Error("no github-actions entry with directory /")
	}

	manifests := 0
	for _, f := range repoFiles(t, "docs") {
		eco, ok := ecosystem(path.Base(f))
		if !ok {
			continue
		}
		manifests++
		dir := path.Dir(f)
		want := "/"
		if dir != "." {
			want = "/" + dir
		}
		if !covered[[2]string{eco, want}] {
			t.Errorf("%s has no Dependabot entry (package-ecosystem %s, directory %s)", f, eco, want)
		}
	}
	if manifests == 0 {
		t.Fatal("no dependency manifest found")
	}
	t.Logf("%d manifests, %d Dependabot entries", manifests, len(cfg.Updates))
}
