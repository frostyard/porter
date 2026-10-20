// Package thread is Porter's one continuous thread (ADR-0006): its turns,
// their full-text index, and the rolling summary of turns that have aged
// out of the model's prompt. It is porterd's own SQLite database, under
// /var/lib/frostporter, never on a network file system.
package thread

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the database/sql driver "sqlite"

	"github.com/frostyard/porter/api"
)

// ErrNotFound means no turn or card has the id asked for.
var ErrNotFound = errors.New("not found")

// Store is the thread's database. It is safe for concurrent use; SQLite
// serializes its writes.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS turns (
	id      INTEGER PRIMARY KEY,
	at      TEXT    NOT NULL,
	role    TEXT    NOT NULL,
	text    TEXT    NOT NULL,
	context TEXT,
	cards   TEXT    NOT NULL DEFAULT '[]',
	done    INTEGER NOT NULL DEFAULT 1
);
CREATE VIRTUAL TABLE IF NOT EXISTS turns_fts USING fts5(text, content='turns', content_rowid='id');
CREATE TRIGGER IF NOT EXISTS turns_ai AFTER INSERT ON turns BEGIN
	INSERT INTO turns_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS turns_au AFTER UPDATE OF text ON turns BEGIN
	INSERT INTO turns_fts(turns_fts, rowid, text) VALUES ('delete', old.id, old.text);
	INSERT INTO turns_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TABLE IF NOT EXISTS summary (
	id   INTEGER PRIMARY KEY CHECK (id = 1),
	upto INTEGER NOT NULL,
	text TEXT    NOT NULL
);
`

// Open opens or creates the thread's database at path. A Porter turn left
// unfinished by a stopped porterd is marked done with a note.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open thread database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("thread schema: %w", err)
	}
	s := &Store{db: db, now: time.Now}
	if _, err := db.ExecContext(ctx, `UPDATE turns SET done = 1,
		text = CASE WHEN text = '' THEN '(Porter stopped before finishing this reply.)' ELSE text END
		WHERE done = 0`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("thread recovery: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Add stores t as a new turn, setting its id and time, and returns it.
func (s *Store) Add(ctx context.Context, t api.Turn) (api.Turn, error) {
	t.At = s.now().UTC().Format(time.RFC3339)
	if t.Cards == nil {
		t.Cards = []api.Card{}
	}
	ctxJSON, cards, err := encode(t)
	if err != nil {
		return t, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO turns (at, role, text, context, cards, done) VALUES (?, ?, ?, ?, ?, ?)`,
		t.At, string(t.Role), t.Text, ctxJSON, cards, t.Done)
	if err != nil {
		return t, fmt.Errorf("add turn: %w", err)
	}
	if t.ID, err = res.LastInsertId(); err != nil {
		return t, fmt.Errorf("add turn: %w", err)
	}
	return t, nil
}

// Update replaces the text, cards and done of the turn with t's id.
func (s *Store) Update(ctx context.Context, t api.Turn) error {
	if t.Cards == nil {
		t.Cards = []api.Card{}
	}
	_, cards, err := encode(t)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE turns SET text = ?, cards = ?, done = ? WHERE id = ?`, t.Text, cards, t.Done, t.ID)
	if err != nil {
		return fmt.Errorf("update turn %d: %w", t.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("turn %d: %w", t.ID, ErrNotFound)
	}
	return nil
}

// Get returns the turn with id.
func (s *Store) Get(ctx context.Context, id int64) (api.Turn, error) {
	ts, err := s.query(ctx, `SELECT id, at, role, text, context, cards, done FROM turns WHERE id = ?`, id)
	if err != nil {
		return api.Turn{}, err
	}
	if len(ts) == 0 {
		return api.Turn{}, fmt.Errorf("turn %d: %w", id, ErrNotFound)
	}
	return ts[0], nil
}

// List returns at most limit turns with ids below before (all, when before
// is 0), oldest first, and whether older ones exist.
func (s *Store) List(ctx context.Context, before int64, limit int) ([]api.Turn, bool, error) {
	if before <= 0 {
		before = 1 << 62
	}
	ts, err := s.query(ctx, `SELECT id, at, role, text, context, cards, done FROM turns
		WHERE id < ? ORDER BY id DESC LIMIT ?`, before, limit+1)
	if err != nil {
		return nil, false, err
	}
	more := len(ts) > limit
	if more {
		ts = ts[:limit]
	}
	reverse(ts)
	return ts, more, nil
}

// Since returns every turn with an id above after, oldest first.
func (s *Store) Since(ctx context.Context, after int64) ([]api.Turn, error) {
	return s.query(ctx, `SELECT id, at, role, text, context, cards, done FROM turns WHERE id > ? ORDER BY id`, after)
}

// Search returns at most limit turns whose text matches every term of
// query, newest first. Terms are matched as words; query syntax is not
// interpreted.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]api.Turn, error) {
	var terms []string
	for _, f := range strings.Fields(query) {
		terms = append(terms, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	if len(terms) == 0 {
		return nil, nil
	}
	return s.query(ctx, `SELECT t.id, t.at, t.role, t.text, t.context, t.cards, t.done
		FROM turns_fts f JOIN turns t ON t.id = f.rowid
		WHERE turns_fts MATCH ? ORDER BY t.id DESC LIMIT ?`, strings.Join(terms, " "), limit)
}

// CardTurn returns the turn holding the card with id.
func (s *Store) CardTurn(ctx context.Context, card string) (api.Turn, error) {
	ts, err := s.query(ctx, `SELECT t.id, t.at, t.role, t.text, t.context, t.cards, t.done
		FROM turns t, json_each(t.cards) c WHERE json_extract(c.value, '$.id') = ? LIMIT 1`, card)
	if err != nil {
		return api.Turn{}, err
	}
	if len(ts) == 0 {
		return api.Turn{}, fmt.Errorf("card %s: %w", card, ErrNotFound)
	}
	return ts[0], nil
}

// Summary returns the rolling summary and the id of the last turn it
// covers; 0 and "" before the first.
func (s *Store) Summary(ctx context.Context) (int64, string, error) {
	var upto int64
	var text string
	err := s.db.QueryRowContext(ctx, `SELECT upto, text FROM summary WHERE id = 1`).Scan(&upto, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("summary: %w", err)
	}
	return upto, text, nil
}

// SetSummary replaces the rolling summary, now covering turns up to upto.
func (s *Store) SetSummary(ctx context.Context, upto int64, text string) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO summary (id, upto, text) VALUES (1, ?, ?)
		ON CONFLICT (id) DO UPDATE SET upto = excluded.upto, text = excluded.text`, upto, text); err != nil {
		return fmt.Errorf("set summary: %w", err)
	}
	return nil
}

func (s *Store) query(ctx context.Context, q string, args ...any) ([]api.Turn, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query turns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ts []api.Turn
	for rows.Next() {
		var t api.Turn
		var role string
		var ctxJSON sql.NullString
		var cards string
		if err := rows.Scan(&t.ID, &t.At, &role, &t.Text, &ctxJSON, &cards, &t.Done); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		t.Role = api.TurnRole(role)
		if ctxJSON.Valid && ctxJSON.String != "" {
			var c api.Context
			if err := json.Unmarshal([]byte(ctxJSON.String), &c); err != nil {
				return nil, fmt.Errorf("turn %d context: %w", t.ID, err)
			}
			t.Context = &c
		}
		if err := json.Unmarshal([]byte(cards), &t.Cards); err != nil {
			return nil, fmt.Errorf("turn %d cards: %w", t.ID, err)
		}
		if t.Cards == nil {
			t.Cards = []api.Card{}
		}
		ts = append(ts, t)
	}
	return ts, rows.Err()
}

func encode(t api.Turn) (ctxJSON sql.NullString, cards string, err error) {
	if t.Context != nil {
		b, err := json.Marshal(t.Context)
		if err != nil {
			return ctxJSON, "", fmt.Errorf("encode context: %w", err)
		}
		ctxJSON = sql.NullString{String: string(b), Valid: true}
	}
	b, err := json.Marshal(t.Cards)
	if err != nil {
		return ctxJSON, "", fmt.Errorf("encode cards: %w", err)
	}
	return ctxJSON, string(b), nil
}

func reverse(ts []api.Turn) {
	for i, j := 0, len(ts)-1; i < j; i, j = i+1, j-1 {
		ts[i], ts[j] = ts[j], ts[i]
	}
}
