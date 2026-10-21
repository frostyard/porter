package tools

import (
	"encoding/json/v2"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// known marks the tools of lodge's apps by what they do. A tool not listed
// here is judged by its annotations: read-only is Read, anything else is
// Change, which routines never get. Apps in readOnlyApps are the exception.
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
		"draft_reply": Draft, "draft_message": Draft,
		"set_flags": Change, "move_messages": Change,
		"create_task": Change, "complete_task": Change, "remind_me": Change,
		"send_draft": Never,
	},
	"read": {
		"list_sources": Read, "list_unread": Read, "search": Read, "read_item": Read,
		"save_url": Never, "mark_read": Never, "star": Never,
	},
}

// readOnlyApps are the apps Porter only reads (ADR-0007). For them known is
// an allow-list: a tool it does not name is Never, whatever its annotations
// say and whatever the token's scope offers, so a wider token or a new tool
// in the app gives Porter no write.
var readOnlyApps = map[string]bool{"read": true}

// Classify returns what app's tool t may do. A tool whose name says it
// sends or posts is Never, whatever else is known of it (ADR-0004). So is
// any tool of a read-only app (readOnlyApps) that known does not mark Read
// (ADR-0007).
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
	if readOnlyApps[app] {
		return Never
	}
	if t.Annotations != nil && t.Annotations.ReadOnlyHint {
		return Read
	}
	return Change
}

// resultURL returns the lodge address a tool's structured result names,
// for the card the owner sees. The rule is the app's:
//
//   - frostread's read_item result has the article's web address in url,
//     which is not a lodge address. The card links /read/item/<id>, which
//     opens the reader, from the result's top-level id. Its other tools
//     give no link.
//   - for every other app, the result's top-level url.
func resultURL(app, tool string, structured []byte) string {
	if app == "read" {
		var item struct {
			ID int64 `json:"id"`
		}
		if tool != "read_item" || json.Unmarshal(structured, &item) != nil || item.ID <= 0 {
			return ""
		}
		return "/read/item/" + strconv.FormatInt(item.ID, 10)
	}
	var top struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(structured, &top) != nil {
		return ""
	}
	return top.URL
}
