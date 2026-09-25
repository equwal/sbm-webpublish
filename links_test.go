package main

import (
	"slices"
	"strings"
	"testing"
	"time"

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

func TestDateLineParsesBack(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// From the year 1 to the year 9999: RFC 3339 has four digits for
		// the year.
		u := time.Unix(rapid.Int64Range(-62135596800, 253402300799).Draw(t, "unix"), rapid.Int64Range(0, 999999999).Draw(t, "ns"))
		got, ok := dateOf(dateLine(u))
		if !ok || !got.Equal(u.Truncate(time.Second)) {
			t.Fatalf("%q gave %v %v", dateLine(u), got, ok)
		}
	})
}

func TestParseDates(t *testing.T) {
	d := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ls := parse("a.com\tA\t\n# 2026-01-02T03:04:05Z\nb.com\tB\t\n# a comment\nc.com\tC\t\n#2026-01-01T00:00:00Z\nd.com\tD\t\n")
	want := []time.Time{{}, d, d, e}
	if len(ls) != len(want) {
		t.Fatalf("got %d links", len(ls))
	}
	for i, l := range ls {
		if !l.Added.Equal(want[i]) {
			t.Errorf("%s: added %v, want %v", l.URL, l.Added, want[i])
		}
	}
}

func TestAddDatedDatesOnlyTheNewLinks(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base, _, _ := merge("", rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "base"))
		rows := rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "rows")
		when := time.Unix(rapid.Int64Range(0, 4102444800).Draw(t, "when"), 0)
		out, added, _ := addDated(base, rows, when)
		old, ls := parse(base), parse(out)
		switch {
		case added == 0 && out != base:
			t.Fatalf("no link added, but the text changed to %q", out)
		case added > 0 && !strings.HasPrefix(out, base+dateLine(when)+"\n"):
			t.Fatalf("no date line after the old links in %q", out)
		case len(ls) != len(old)+added:
			t.Fatalf("%d links, want %d + %d", len(ls), len(old), added)
		}
		for i, l := range ls {
			if i < len(old) && !l.Added.Equal(old[i].Added) || i >= len(old) && !l.Added.Equal(when) {
				t.Fatalf("link %d %q has the time %v", i, l.URL, l.Added)
			}
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
