package agent

import (
	"regexp"
	"strings"

	"github.com/frostyard/porter/api"
)

// A claim is an action a reply's text says Porter took, and the tool cards
// that would show it did. In its first live use, the local model wrote
// "Draft ready" four times without calling a tool, so porterd checks every
// reply's claims against its cards instead of trusting the text.
type claim struct {
	name  string
	said  *regexp.Regexp
	shown func(api.Card) bool
}

var claims = []claim{
	{"draft", regexp.MustCompile(`(?i)\b(draft (is |was )?(ready|created|saved|written|waiting)|(created|made|wrote|written|saved|started)( you)? (a |the |another |new |fresh |second )*drafts?\b|drafted|waiting in (your )?drafts|(new|fresh) draft (created|ready))`),
		func(c api.Card) bool { return c.Status == api.StatusDone && strings.Contains(c.Tool, "draft") }},
	{"capture", regexp.MustCompile(`(?i)\b(captured|appended|(added|staged) (it |this |that |the [a-z ]+ )?(to|in|into) (today's|your) (daily )?note)`),
		func(c api.Card) bool { return c.Kind == api.CardCapture }},
	{"remember", regexp.MustCompile(`(?i)\b((added|saved|noted|put|wrote) (it |that |this )?(to|in|into) (your |my )?memory|i('ll| will) remember|remembered)\b`),
		func(c api.Card) bool { return c.Status == api.StatusDone && c.Tool == "remember" }},
	{"task", regexp.MustCompile(`(?i)\b((created|added|made|set up) (a |the |your )?(new )?(task|reminder)|task (is )?(created|added))\b`),
		func(c api.Card) bool {
			return c.Status == api.StatusDone && (c.Tool == "create_task" || c.Tool == "remind_me")
		}},
}

// unbacked returns the names of the actions text claims that none of
// cards shows.
func unbacked(text string, cards []api.Card) []string {
	var out []string
	for _, cl := range claims {
		if !cl.said.MatchString(text) {
			continue
		}
		backed := false
		for _, c := range cards {
			if cl.shown(c) {
				backed = true
				break
			}
		}
		if !backed {
			out = append(out, cl.name)
		}
	}
	return out
}

// nudge is what the model reads when its answer claims actions no tool
// took.
func nudge(names []string) string {
	return "Check before you answer: your reply says you did this (" + strings.Join(names, ", ") +
		"), but you called no tool that did it in this reply, so nothing was done. " +
		"If the owner asked for it, call the tool now. Otherwise answer again without claiming it."
}

// unbackedNote is added to a reply whose claims stayed unbacked after the
// nudge, so the owner is never told something was done that was not.
const unbackedNote = "\n\n_Porter wrote that it did something here, but it called no tool to do it, so nothing was done._"
