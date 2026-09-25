package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// genRow makes a file line: a URL, a description, tags and sometimes a feed,
// with tabs, spaces and line breaks in awkward places.
func genRow(t *rapid.T) string {
	host := rapid.SampledFrom([]string{"a.com", "www.a.com", "b.org", "c.net"}).Draw(t, "host")
	path := rapid.SampledFrom([]string{"", "/", "/x", "/x/", "/y"}).Draw(t, "path")
	sch := rapid.SampledFrom([]string{"https://", "http://", ""}).Draw(t, "scheme")
	desc := rapid.StringMatching(`[a-z \t\n]{0,8}`).Draw(t, "desc")
	tags := rapid.StringMatching(`[a-z ]{0,8}`).Draw(t, "tags")
	feed := rapid.SampledFrom([]string{"", "", "https://a.com/feed", "b.org/rss", " c.net/atom.xml "}).Draw(t, "feed")
	return line(sch+host+path, desc, tags, feed)
}

// countDates gives the number of date lines of text.
func countDates(text string) int {
	n := 0
	for _, s := range lines(text) {
		if _, ok := dateOf(s); ok {
			n++
		}
	}
	return n
}

func TestLineParsesBack(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapid.StringMatching(`[a-z:/. ]{1,12}`).Draw(t, "url")
		desc := rapid.String().Draw(t, "desc")
		tags := rapid.StringMatching(`[a-z \t]{0,12}`).Draw(t, "tags")
		feed := rapid.StringMatching(`[a-z:/. \t]{0,12}`).Draw(t, "feed")
		row := line(u, desc, tags, feed)
		if row == "" {
			return
		}
		// A line without a feed has the three fields of bm.
		tabs := 2
		if squeeze(feed) != "" {
			tabs = 3
		}
		if strings.ContainsAny(row, "\n\r") || strings.Count(row, "\t") != tabs {
			t.Fatalf("bad line %q", row)
		}
		l, ok := parseLine(row)
		if !ok || l.URL != squeeze(u) || !slices.Equal(l.Tags, strings.Fields(tags)) || l.Feed != squeeze(feed) {
			t.Fatalf("line %q parsed as %+v", row, l)
		}
	})
}

func TestMergeKeepsOneLineForEachURL(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base, _, _, _ := merge("", rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "base"))
		rows := rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "rows")
		text, added, skipped, _ := merge(base, rows)
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
		if again, n, _, f := merge(text, rows); n != 0 || f != 0 || again != text {
			t.Fatalf("second merge changed the file")
		}
	})
}

// TestMergeFeeds: after merge, each link has the first feed of its URL: the
// feed of its line in the text, else the feed of the first row with that
// URL that has a feed.
func TestMergeFeeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base, _, _, _ := merge("", rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "base"))
		rows := rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "rows")
		out, _, _, feeds := merge(base, rows)
		want, seen, filled := map[string]string{}, map[string]bool{}, 0
		for _, s := range append(lines(base), rows...) {
			k := key(s)
			l, _ := parseLine(s)
			switch {
			case k == "":
			case !seen[k]:
				seen[k], want[k] = true, l.Feed
			case want[k] == "" && l.Feed != "":
				want[k] = l.Feed
				filled++
			}
		}
		if feeds != filled {
			t.Fatalf("merge gave %d feeds, want %d", feeds, filled)
		}
		for _, s := range lines(out) {
			if l, _ := parseLine(s); l.Feed != want[key(s)] {
				t.Fatalf("%q has the feed %q, want %q", s, l.Feed, want[key(s)])
			}
		}
	})
}

func TestSetFeed(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text, _, _, _ := merge("", rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "rows"))
		k := key(genRow(t))
		feed := rapid.SampledFrom([]string{"", "https://x.org/feed", " y.org/rss "}).Draw(t, "feed")
		old, ls := lines(text), lines(setFeed(text, k, feed))
		if len(ls) != len(old) {
			t.Fatalf("%d lines, want %d", len(ls), len(old))
		}
		for i := range old {
			a, _ := parseLine(old[i])
			b, _ := parseLine(ls[i])
			want := a.Feed
			if key(old[i]) == k {
				want = squeeze(feed)
			}
			if b.URL != a.URL || b.Desc != a.Desc || !slices.Equal(b.Tags, a.Tags) || b.Feed != want {
				t.Fatalf("line %q became %q, want the feed %q", old[i], ls[i], want)
			}
		}
	})
}

func TestRemove(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		text, _, _, _ := merge("", rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "rows"))
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
		base, _, _, _ := merge("", rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "base"))
		rows := rapid.SliceOf(rapid.Custom(genRow)).Draw(t, "rows")
		when := time.Unix(rapid.Int64Range(0, 4102444800).Draw(t, "when"), 0)
		out, added, _, feeds := addDated(base, rows, when)
		old, ls := parse(base), parse(out)
		dates := countDates(base)
		if added > 0 {
			dates++
		}
		switch {
		case added == 0 && feeds == 0 && out != base:
			t.Fatalf("no link and no feed added, but the text changed to %q", out)
		case countDates(out) != dates:
			t.Fatalf("%d date lines in %q, want %d", countDates(out), out, dates)
		case len(ls) != len(old)+added:
			t.Fatalf("%d links, want %d + %d", len(ls), len(old), added)
		}
		for i, l := range ls {
			if i < len(old) && (l.URL != old[i].URL || !l.Added.Equal(old[i].Added)) || i >= len(old) && !l.Added.Equal(when) {
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
		"":                      "",
		"javascript:alert(1)":   "",
		"data:text/html,x":      "",
		"ftp://a.com":           "",
		"JavaScript:alert(1)//": "",
	} {
		if got := (Link{URL: u}).Href(); got != want {
			t.Errorf("Href(%q) = %q, want %q", u, got, want)
		}
		if got := (Link{Feed: u}).FeedHref(); got != want {
			t.Errorf("FeedHref(%q) = %q, want %q", u, got, want)
		}
	}
}

func TestAddress(t *testing.T) {
	for u, want := range map[string]string{
		"https://github.com/equwal/sbm/": "github.com/equwal/sbm",
		"http://a.com":                   "a.com",
		"a.com/x?y=1":                    "a.com/x?y=1",
	} {
		if got := (Link{URL: u}).Address(); got != want {
			t.Errorf("Address(%q) = %q, want %q", u, got, want)
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
	text, added, _, _ := merge("", []string{"https://a.com some site"})
	if added != 1 || text != "https://a.com\tsome site\t\n" {
		t.Fatalf("got %q", text)
	}
}
