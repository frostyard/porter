package agent

import (
	"encoding/json/v2"
	"regexp"
	"strconv"
	"strings"

	"github.com/frostyard/porter/api"
)

// idSet holds the ids a tool call that changes something may use. The
// local model guessed ids even when told not to (draft_reply on message 1,
// another person's thread), so such a call takes only an id that a tool
// returned in this reply, that the owner wrote, that the screen shows, or
// that an earlier call used and succeeded with.
type idSet map[string]bool

// isIDKey says whether an argument or result key names an id.
func isIDKey(k string) bool {
	return k == "id" || k == "ids" || strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_ids")
}

var digits = regexp.MustCompile(`\d+`)

// addJSON adds the ids in a JSON text: every value of an id key.
func (s idSet) addJSON(text string) {
	var v any
	if json.Unmarshal([]byte(text), &v) == nil {
		s.walk(v, false, nil)
	}
}

// addWords adds the numbers in text, such as the owner's words.
func (s idSet) addWords(text string) {
	for _, d := range digits.FindAllString(text, -1) {
		s[d] = true
	}
}

// unseen returns the ids in args the set lacks, as "key value".
func (s idSet) unseen(args map[string]any) []string {
	var out []string
	for k, v := range args {
		s.walk(v, isIDKey(k), func(id string) {
			if !s[id] {
				out = append(out, k+" "+id)
			}
		})
	}
	return out
}

// walk visits the id values in v; with check nil it adds them.
func (s idSet) walk(v any, isID bool, check func(string)) {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			s.walk(x, isIDKey(k), check)
		}
	case []any:
		for _, x := range v {
			s.walk(x, isID, check)
		}
	case float64, string:
		if !isID {
			return
		}
		id := idString(v)
		if id == "" || id == "0" {
			return // unset
		}
		if check == nil {
			s[id] = true
		} else {
			check(id)
		}
	}
}

func idString(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// knownIDs seeds a reply's ids: the owner's words, what is on screen, and
// the arguments of the history's calls that succeeded.
func knownIDs(owner api.Turn, msgs []Message) idSet {
	s := idSet{}
	s.addWords(owner.Text)
	if c := owner.Context; c != nil && c.Ref != "" {
		s[c.Ref] = true
		s.addWords(c.Ref)
	}
	done := map[string]bool{}
	for _, m := range msgs {
		if m.Role == "tool" && m.Content != nil && strings.HasPrefix(*m.Content, "Done:") {
			done[m.ToolCallID] = true
		}
	}
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			if done[c.ID] {
				s.addJSON(c.Function.Arguments)
			}
		}
	}
	return s
}
