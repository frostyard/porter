// Package links rewrites the links in text Porter writes into the vault.
// Porter's answers link by lodge address (/notes/2/Plan.md, /mail/message/…),
// which its thread opens through lodge. In a note those read as paths in
// the vault: a click made a note named mail/message/… So text bound for the
// vault links notes as wikilinks, which notes and Obsidian both resolve, and
// everything else in lodge by its absolute URL.
package links

import (
	"net/url"
	"regexp"
	"strings"
)

// mdLink is a markdown link to a rooted path: [text](/path), not an image
// and not a protocol-relative //host.
var mdLink = regexp.MustCompile(`(^|[^!\\])\[([^\]\n]*)\]\((/[^/\s)][^\s)]*)\)`)

// notePath is a lodge address of a note: /notes/<vault id>/<path>.
var notePath = regexp.MustCompile(`^/notes/[0-9]+/(.+)$`)

// ForVault rewrites text's links to lodge addresses for a note: a link to a
// note becomes a wikilink, and any other lodge address becomes an absolute
// URL under lodgeURL (when it is set). Fenced code is left as it is.
func ForVault(text, lodgeURL string) string {
	if !strings.Contains(text, "](/") {
		return text
	}
	lodgeURL = strings.TrimRight(lodgeURL, "/")
	lines := strings.SplitAfter(text, "\n")
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if f := fenceOf(trimmed); f != "" {
			switch {
			case fence == "":
				fence = f
			case strings.HasPrefix(trimmed, fence):
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		// Twice: a match takes the character before its link, so a link
		// right after another is found on the second pass.
		for range 2 {
			line = mdLink.ReplaceAllStringFunc(line, func(m string) string {
				sub := mdLink.FindStringSubmatch(m)
				return sub[1] + rewrite(sub[2], sub[3], lodgeURL)
			})
		}
		lines[i] = line
	}
	return strings.Join(lines, "")
}

func fenceOf(line string) string {
	for _, f := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, f) {
			return f
		}
	}
	return ""
}

// rewrite returns the link for one [text](target).
func rewrite(text, target, lodgeURL string) string {
	if m := notePath.FindStringSubmatch(target); m != nil {
		p, frag, _ := strings.Cut(m[1], "#")
		if decoded, err := url.PathUnescape(p); err == nil && frag == "" {
			return wikilink(text, decoded)
		}
	}
	if lodgeURL == "" {
		return "[" + text + "](" + target + ")"
	}
	return "[" + text + "](" + lodgeURL + target + ")"
}

// wikilink links a note by its vault path, with text as the alias unless
// it only repeats the name. A path or text a wikilink cannot carry becomes
// a markdown link by the vault path, which notes resolves as a whole-path
// suffix.
func wikilink(text, p string) string {
	if strings.ContainsAny(p, "[]|#^\n") || strings.ContainsAny(text, "[]|\n") {
		return "[" + text + "](" + (&url.URL{Path: p}).EscapedPath() + ")"
	}
	name := strings.TrimSuffix(p, ".md")
	base := name[strings.LastIndex(name, "/")+1:]
	if text == "" || text == name || text == base || text == p {
		return "[[" + name + "]]"
	}
	return "[[" + name + "|" + text + "]]"
}
