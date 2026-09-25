package main

import (
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// genRow makes a file line: a URL, a description and tags, with tabs,
// spaces and line breaks in awkward places.
func genRow(t *rapid.T) string {
	host := rapid.SampledFrom([]string{"a.com", "www.a.com", "b.org", "c.net"}).Draw(t, "host")
	path := rapid.SampledFrom([]string{"", "/", "/x", "/x/", "/y"}).Draw(t, "path")
	sch := rapid.SampledFrom([]string{"https://", "http://", ""}).Draw(t, "scheme")
	desc := rapid.StringMatching(`[a-z \t\n]{0,8}`).Draw(t, "desc")
	tags := rapid.StringMatching(`[a-z ]{0,8}`).Draw(t, "tags")
	return line(sch+host+path, desc, tags)
}

func TestLineParsesBack(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapid.StringMatching(`[a-z:/. ]{1,12}`).Draw(t, "url")
		desc := rapid.String().Draw(t, "desc")
		tags := rapid.StringMatching(`[a-z \t]{0,12}`).Draw(t, "tags")
		row := line(u, desc, tags)
		if row == "" {
			return
		}
		if strings.ContainsAny(row, "\n\r") || strings.Count(row, "\t") != 2 {
			t.Fatalf("bad line %q", row)
		}
		l, ok := parseLine(row)
		if !ok || l.URL != strings.Join(strings.Fields(u), "") || !slices.Equal(l.Tags, strings.Fields(tags)) {
			t.Fatalf("line %q parsed as %+v", row, l)
		}
	})
}

func TestMergeKeepsOneLineForEachURL(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base, _, _ := merge("", rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "base"))
		rows := rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "rows")
		text, added, skipped := merge(base, rows)
		if added+skipped != len(rows) {
			t.Fatalf("added %d + skipped %d != %d rows", added, skipped, len(rows))
		}
		seen := map[string]bool{}
		for _, s := range lines(text) {
			if seen[key(s)] {
				t.Fatalf("duplicate %q in %q", key(s), text)
			}
			seen[key(s)] = true
		}
		for _, r := range rows {
			if !seen[key(r)] {
				t.Fatalf("row %q is missing", r)
			}
		}
		if again, n, _ := merge(text, rows); n != 0 || again != text {
			t.Fatalf("second merge changed the file")
		}
	})
}

func TestRemove(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text, _, _ := merge("", rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "rows"))
		k := key(genRow(t))
		out := remove(text, k)
		var want []string
		for _, s := range lines(text) {
			if key(s) != k {
				want = append(want, s)
			}
		}
		if out != join(want) {
			t.Fatalf("remove %q from %q gave %q", k, text, out)
		}
	})
}

func TestHrefIsOnlyHTTP(t *testing.T) {
	for u, want := range map[string]string{
		"https://a.com":         "https://a.com",
		"HTTP://a.com":          "HTTP://a.com",
		"a.com/x":               "https://a.com/x",
		"javascript:alert(1)":   "",
		"data:text/html,x":      "",
		"ftp://a.com":           "",
		"JavaScript:alert(1)//": "",
	} {
		if got := (Link{URL: u}).Href(); got != want {
			t.Errorf("Href(%q) = %q, want %q", u, got, want)
		}
	}
}

func TestFromHTML(t *testing.T) {
	in := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
<DT><H3>Dev Tools</H3>
<DL><p>
<DT><A HREF="https://go.dev/" ADD_DATE="1">Go &amp; more</A>
<DT><A HREF="javascript:x">bad</A>
</DL><p>
<DT><A HREF="https://a.com">A</A>
</DL>`
	want := []string{"https://go.dev/\tGo & more\tdev-tools", "https://a.com\tA\t"}
	if got := importRows(in); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMergeOldFormat(t *testing.T) {
	text, added, _ := merge("", []string{"https://a.com some site"})
	if added != 1 || text != "https://a.com\tsome site\t\n" {
		t.Fatalf("got %q", text)
	}
}
