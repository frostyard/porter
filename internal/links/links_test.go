package links

import "testing"

func TestForVaultRewritesLodgeLinks(t *testing.T) {
	const lodge = "https://lodge.example.ts.net"
	for _, c := range []struct{ in, want string }{
		{"See [Re: Aurora](/mail/message/aurora-3%40example.com).",
			"See [Re: Aurora](https://lodge.example.ts.net/mail/message/aurora-3%40example.com)."},
		{"Notes: [Anh, 19 October](/notes/2/Work/People/Anh/2026-10-19.md)",
			"Notes: [[Work/People/Anh/2026-10-19|Anh, 19 October]]"},
		{"[2026-10-19](/notes/2/Journal/2026-10-19.md) and [Plan](/notes/2/Projects/Big%20Plan.md)",
			"[[Journal/2026-10-19]] and [[Projects/Big Plan|Plan]]"},
		{"[the map](/notes/2/Maps/soil%20map.png)", "[[Maps/soil map.png|the map]]"},
		{"[odd](/notes/2/A%5Bb%5D.md)", "[odd](A%5Bb%5D.md)"},
		{"[section](/notes/2/Plan.md#Goals)", "[section](https://lodge.example.ts.net/notes/2/Plan.md#Goals)"},
		{"[GitHub](https://github.com/x) stays, and so does ![img](/mail/x.png)",
			"[GitHub](https://github.com/x) stays, and so does ![img](/mail/x.png)"},
		{"[protocol-relative](//evil.example/x) stays", "[protocol-relative](//evil.example/x) stays"},
		{"```\n[code](/mail/message/1)\n```\n[out](/mail/message/1)\n",
			"```\n[code](/mail/message/1)\n```\n[out](https://lodge.example.ts.net/mail/message/1)\n"},
		{"[a](/mail/message/1)[b](/notes/2/B.md)", "[a](https://lodge.example.ts.net/mail/message/1)[[B|b]]"},
		{"No links at all.", "No links at all."},
	} {
		if got := ForVault(c.in, lodge); got != c.want {
			t.Errorf("ForVault(%q)\n = %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestWithoutALodgeURLOnlyNotesChange(t *testing.T) {
	got := ForVault("[a](/mail/message/1) [b](/notes/2/B.md)", "")
	if want := "[a](/mail/message/1) [[B|b]]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
