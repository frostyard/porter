package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAndValidation(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || c.ChatAlias != "local" || c.RoutineAlias != "local" || strings.Join(c.Apps, ",") != "notes,mail,read" {
		t.Fatalf("defaults = %+v, %v", c, err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown.json": `{"owner":"Ann","extra":1}`,
		"zone.json":    `{"time_zone":"Mars/Olympus"}`,
		"app.json":     `{"apps":["Notes"]}`,
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("%s loaded", name)
		}
	}
	p := filepath.Join(dir, "good.json")
	if err := os.WriteFile(p, []byte(`{"owner":"Ann","time_zone":"America/New_York","apps":["notes"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = Load(p)
	if err != nil || c.Owner != "Ann" || len(c.Apps) != 1 {
		t.Fatalf("good = %+v, %v", c, err)
	}
	if loc, _ := c.Location(); loc.String() != "America/New_York" {
		t.Fatalf("location = %v", loc)
	}
}

func TestTokens(t *testing.T) {
	state := t.TempDir()
	if tok, err := Token(state, "notes"); err != nil || tok != "" {
		t.Fatalf("no token = %q, %v", tok, err)
	}
	good := "lgm_" + strings.Repeat("A", 43)
	for _, bad := range []string{"", "lgm_short", "sk-" + strings.Repeat("A", 44), good + "x"} {
		if err := SetToken(state, "notes", bad); err == nil {
			t.Errorf("SetToken(%q) succeeded", bad)
		}
	}
	if err := SetToken(state, "../etc", good); err == nil {
		t.Error("a bad app id was accepted")
	}
	if err := SetToken(state, "notes", good+"\n"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(TokenPath(state, "notes"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file = %v, %v", fi.Mode(), err)
	}
	if tok, _ := Token(state, "notes"); tok != good {
		t.Fatalf("token = %q", tok)
	}
}

func TestLodgeURLMustBeAnHTTPSOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://lodge.example.ts.net":   "https://lodge.example.ts.net",
		"https://lodge.example.ts.net/":  "https://lodge.example.ts.net",
		"http://lodge.example.ts.net":    "",
		"https://lodge.example.ts.net/x": "",
		"lodge.example.ts.net":           "",
	} {
		p := filepath.Join(t.TempDir(), "porter.json")
		if err := os.WriteFile(p, []byte(`{"lodge_url":"`+in+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		switch {
		case want == "" && err == nil:
			t.Errorf("%q was accepted", in)
		case want != "" && (err != nil || c.LodgeURL != want):
			t.Errorf("%q: %q, %v", in, c.LodgeURL, err)
		}
	}
}
