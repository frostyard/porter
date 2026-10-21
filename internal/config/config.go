// Package config is porterd's configuration, /etc/porter/porter.json, and
// its state directory: the thread's database and each app's token.
package config

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultPath is the configuration file, unless PORTER_CONFIG names another.
const DefaultPath = "/etc/porter/porter.json"

// Config is porter.json. Every field may be left out.
type Config struct {
	// Owner is the owner's name, for the system prompt.
	Owner string `json:"owner"`
	// TimeZone is an IANA zone for the owner's dates; the host's when empty.
	TimeZone string `json:"time_zone"`
	// ChatAlias is the inference alias chat uses; "local" when empty.
	ChatAlias string `json:"chat_alias"`
	// RoutineAlias is the alias routines use by default; "local" when empty.
	RoutineAlias string `json:"routine_alias"`
	// Apps are the lodge apps Porter reaches, each with a token in the state
	// directory; notes and mail when empty.
	Apps []string `json:"apps"`
	// LodgeURL is lodge's address, such as https://lodge.example.ts.net:
	// links Porter writes into notes point there. Without it, links to
	// mail stay lodge addresses, which a note cannot follow.
	LodgeURL string `json:"lodge_url"`
}

// Load reads the configuration at path, or PORTER_CONFIG, or DefaultPath.
// A missing file gives the defaults.
func Load(path string) (Config, error) {
	if path == "" {
		path = os.Getenv("PORTER_CONFIG")
	}
	if path == "" {
		path = DefaultPath
	}
	var c Config
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return c, fmt.Errorf("read %s: %w", path, err)
	default:
		if err := json.Unmarshal(b, &c, json.RejectUnknownMembers(true)); err != nil {
			return c, fmt.Errorf("%s: %w", path, err)
		}
	}
	if c.ChatAlias == "" {
		c.ChatAlias = "local"
	}
	if c.RoutineAlias == "" {
		c.RoutineAlias = "local"
	}
	if len(c.Apps) == 0 {
		c.Apps = []string{"notes", "mail"}
	}
	for _, a := range c.Apps {
		if !appID.MatchString(a) {
			return c, fmt.Errorf("%s: app %q is not an app id", path, a)
		}
	}
	if _, err := c.Location(); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if c.LodgeURL != "" {
		u, err := url.Parse(c.LodgeURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("%s: lodge_url %q is not an https origin, such as https://lodge.example.ts.net", path, c.LodgeURL)
		}
		c.LodgeURL = "https://" + u.Host
	}
	return c, nil
}

// Location is the owner's time zone.
func (c Config) Location() (*time.Location, error) {
	if c.TimeZone == "" {
		return time.Local, nil
	}
	return time.LoadLocation(c.TimeZone)
}

var appID = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)

// StateDir is porterd's state directory: STATE_DIRECTORY, which systemd
// sets from StateDirectory=, else /var/lib/frostporter.
func StateDir() string {
	if d := os.Getenv("STATE_DIRECTORY"); d != "" {
		d, _, _ = strings.Cut(d, ":")
		return d
	}
	return "/var/lib/frostporter"
}

// TokenPath is where app's token is kept in the state directory.
func TokenPath(state, app string) string { return filepath.Join(state, "tokens", app) }

// Token reads app's token; "" when it has none.
func Token(state, app string) (string, error) {
	b, err := os.ReadFile(TokenPath(state, app))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("token for %s: %w", app, err)
	}
	return strings.TrimSpace(string(b)), nil
}

var token = regexp.MustCompile(`^lgm_[A-Za-z0-9_-]{43}$`)

// SetToken writes app's token, 0600, replacing any before it.
func SetToken(state, app, tok string) error {
	if !appID.MatchString(app) {
		return fmt.Errorf("%q is not an app id", app)
	}
	tok = strings.TrimSpace(tok)
	if !token.MatchString(tok) {
		return errors.New("that is not a lodge MCP token (lgm_ and 43 characters)")
	}
	dir := filepath.Dir(TokenPath(state, app))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(tok + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), TokenPath(state, app))
}
