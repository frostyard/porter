package tools

import (
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// known marks the tools of lodge's first apps by what they do. A tool not
// listed here is judged by its annotations: read-only is Read, anything
// else is Change, which routines never get.
var known = map[string]map[string]Kind{
	"notes": {
		"list_notes": Read, "read_note": Read, "search_notes": Read, "get_backlinks": Read,
		"create_note": Write, "edit_note": Write,
		"append_to_daily_note": Capture,
	},
	"mail": {
		"list_mailboxes": Read, "search_messages": Read, "read_thread": Read,
		"list_collections": Read, "list_events": Read, "read_event": Read,
		"list_tasks": Read, "read_task": Read, "search_contacts": Read, "read_contact": Read,
		"threads_awaiting_reply": Read, "threads_awaiting_others": Read,
		"draft_reply": Draft,
		"set_flags":   Change, "move_messages": Change,
		"create_task": Change, "complete_task": Change, "remind_me": Change,
		"send_draft": Never,
	},
}

// Classify returns what app's tool t may do. A tool whose name says it
// sends or posts is Never, whatever else is known of it (ADR-0004).
func Classify(app string, t *mcp.Tool) Kind {
	name := strings.ToLower(t.Name)
	for _, w := range []string{"send", "post", "publish", "forward", "invite", "reply_all"} {
		if strings.Contains(name, w) {
			return Never
		}
	}
	if k, ok := known[app][t.Name]; ok {
		return k
	}
	if t.Annotations != nil && t.Annotations.ReadOnlyHint {
		return Read
	}
	return Change
}
