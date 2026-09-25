package main

import (
	"bytes"
	"encoding/xml"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// day is the time of the clock of the test server.
var day = time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)

const dayLine = "# 2026-09-25T08:00:00Z\n"

func newServer(t *testing.T) (*server, http.Handler) {
	s := &server{
		links:    &store{path: filepath.Join(t.TempDir(), "links.sbm")},
		password: "pw",
		now:      func() time.Time { return day },
	}
	return s, s.routes()
}

func do(h http.Handler, r *http.Request, auth bool) *httptest.ResponseRecorder {
	if auth {
		r.SetBasicAuth("me", "pw")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	return do(h, httptest.NewRequest("GET", path, nil), false)
}

func form(path string, v url.Values) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// fataler is *testing.T or *rapid.T.
type fataler interface {
	Fatalf(format string, args ...any)
}

func write(t fataler, s *server, text string) {
	if err := os.WriteFile(s.links.path, []byte(text), 0o644); err != nil {
		t.Fatalf("%v", err)
	}
}

func feedOf(t fataler, w *httptest.ResponseRecorder) atomFeed {
	var f atomFeed
	if err := xml.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatalf("%v in %s", err, w.Body)
	}
	return f
}

func TestAdminNeedsPassword(t *testing.T) {
	_, h := newServer(t)
	for _, r := range []*http.Request{
		httptest.NewRequest("GET", "/links/admin", nil),
		form("/links/admin/add", url.Values{"url": {"https://a.com"}}),
		form("/links/admin/feed", url.Values{"url": {"https://a.com"}, "feed": {"https://a.com/rss"}}),
	} {
		if w := do(h, r, false); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: code %d", r.Method, r.URL, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/links/admin", nil)
	r.SetBasicAuth("me", "wrong")
	if w := do(h, r, false); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: code %d", w.Code)
	}
}

func TestAddAndDelete(t *testing.T) {
	s, h := newServer(t)
	if body := get(h, "/links/links.txt").Body.String(); body != "" {
		t.Fatalf("empty store gave %q", body)
	}
	do(h, form("/links/admin/add", url.Values{"url": {"https://a.com"}, "desc": {"A"}, "tags": {"x y"}, "feed": {"https://a.com/rss"}}), true)
	w := do(h, form("/links/admin/add", url.Values{"url": {"b.com"}}), true)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/links/admin?m=Added." {
		t.Fatalf("add: %d %q", w.Code, w.Header().Get("Location"))
	}
	w = do(h, form("/links/admin/add", url.Values{"url": {"http://www.a.com/"}}), true)
	if !strings.Contains(w.Header().Get("Location"), "already") {
		t.Fatalf("duplicate add: %q", w.Header().Get("Location"))
	}
	if body := get(h, "/links/links.txt").Body.String(); body != "https://a.com\tA\tx y\thttps://a.com/rss\nhttps://b.com\t\t\n" {
		t.Fatalf("links.txt: %q", body)
	}
	do(h, form("/links/admin/delete", url.Values{"url": {"https://b.com"}}), true)
	// The date line of the deleted link stays. bm ignores it.
	b, _ := os.ReadFile(s.links.path)
	if string(b) != dayLine+"https://a.com\tA\tx y\thttps://a.com/rss\n"+dayLine {
		t.Fatalf("file: %q", b)
	}
}

func location(w *httptest.ResponseRecorder) string {
	m, _ := url.ParseQuery(strings.TrimPrefix(w.Header().Get("Location"), "/links/admin?"))
	return m.Get("m")
}

func TestAddGivesTheFeedToALinkWithout(t *testing.T) {
	s, h := newServer(t)
	do(h, form("/links/admin/add", url.Values{"url": {"https://a.com"}, "desc": {"A"}}), true)
	w := do(h, form("/links/admin/add", url.Values{"url": {"a.com/"}, "desc": {"other"}, "feed": {"https://a.com/rss"}}), true)
	if m := location(w); m != "That URL is a link already. It got the feed." {
		t.Fatalf("message %q", m)
	}
	// One date line only: no link was added the second time.
	if b, _ := os.ReadFile(s.links.path); string(b) != dayLine+"https://a.com\tA\t\thttps://a.com/rss\n" {
		t.Fatalf("file: %q", b)
	}
}

func TestAdminPageShowsFeeds(t *testing.T) {
	s, h := newServer(t)
	write(t, s, "https://github.com/a/b\tB\tx\thttps://github.com/a/b/releases.atom\nhttps://c.com\tC\t\n")
	body := do(h, httptest.NewRequest("GET", "/links/admin", nil), true).Body.String()
	for _, want := range []string{
		`<input type="url" name="feed" placeholder="Feed (RSS or Atom) of the site, if it has one">`,
		`<span class="host">github.com/a/b</span>`,
		`<input type="url" name="feed" value="https://github.com/a/b/releases.atom" placeholder="Feed (RSS or Atom)">`,
		`<input type="url" name="feed" value="" placeholder="Feed (RSS or Atom)">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("admin page has no %s", want)
		}
	}
}

func TestEditFeed(t *testing.T) {
	s, h := newServer(t)
	write(t, s, dayLine+"https://a.com\tA\tx\nhttps://b.com\tB\t\n")
	for _, c := range []struct{ feed, message, file string }{
		{"https://a.com/rss", "Saved the feed.", dayLine + "https://a.com\tA\tx\thttps://a.com/rss\nhttps://b.com\tB\t\n"},
		{"javascript:alert(1)", "Give an http or https address for the feed.", dayLine + "https://a.com\tA\tx\thttps://a.com/rss\nhttps://b.com\tB\t\n"},
		{"", "Saved the feed.", dayLine + "https://a.com\tA\tx\nhttps://b.com\tB\t\n"},
	} {
		w := do(h, form("/links/admin/feed", url.Values{"url": {"https://a.com"}, "feed": {c.feed}}), true)
		b, _ := os.ReadFile(s.links.path)
		if m := location(w); m != c.message || string(b) != c.file {
			t.Errorf("feed %q: message %q, file %q", c.feed, m, b)
		}
	}
}

func TestOutputsHideUnsafeLinks(t *testing.T) {
	s, h := newServer(t)
	write(t, s, "javascript:alert(1)\tx\t\ndata:text/html,x\ty\t\nhttps://a.com\tA\t\nhttps://b.com\tB\t\tjavascript:alert(2)\n")
	for _, path := range []string{"/links/links.txt", "/links/atom.xml", "/links/list.html"} {
		body := get(h, path).Body.String()
		if strings.Contains(body, "javascript") || strings.Contains(body, "data:") || !strings.Contains(body, "https://a.com") || !strings.Contains(body, "https://b.com") {
			t.Errorf("%s: %s", path, body)
		}
	}
}

func TestAtom(t *testing.T) {
	s, h := newServer(t)
	write(t, s, "https://old.com\tOld\t\n# 2026-01-02T03:04:05Z\nhttps://a.com\tA & B\tx y\n# a comment\nb.com\t\t\n")
	r := httptest.NewRequest("GET", "/links/atom.xml", nil)
	r.Host = "example.org"
	r.Header.Set("X-Forwarded-Proto", "https")
	w := do(h, r, false)
	if ct := w.Header().Get("Content-Type"); ct != "application/atom+xml; charset=utf-8" {
		t.Errorf("Content-Type %q", ct)
	}
	f := feedOf(t, w)
	self := "https://example.org/links/atom.xml"
	if f.ID != self || f.Link != (atomLink{"self", self}) || f.Title != "example.org links" || f.Author != "example.org" {
		t.Errorf("feed: %+v", f)
	}
	st, err := os.Stat(s.links.path)
	if err != nil {
		t.Fatal(err)
	}
	mod := atomTime(st.ModTime())
	if f.Updated != mod {
		t.Errorf("feed updated %q, want the time of the file %q", f.Updated, mod)
	}
	date := "2026-01-02T03:04:05Z"
	want := []atomEntry{
		{Title: "https://b.com", Link: atomLink{Href: "https://b.com"}, ID: "https://b.com", Updated: date},
		{Title: "A & B", Link: atomLink{Href: "https://a.com"}, ID: "https://a.com", Updated: date,
			Categories: []atomCategory{{"x"}, {"y"}}},
		{Title: "Old", Link: atomLink{Href: "https://old.com"}, ID: "https://old.com", Updated: mod},
	}
	if !reflect.DeepEqual(f.Entries, want) {
		t.Errorf("entries:\n%+v\nwant:\n%+v", f.Entries, want)
	}
}

// genPublicRow makes a file line with the characters that XML and HTML must
// escape, and with links that the outputs must leave out.
func genPublicRow(t *rapid.T) string {
	u := rapid.SampledFrom([]string{"https://a.com/", "http://b.org/?x=1&y=<2>", "c.net/", "javascript:alert(1)//", "ftp://d.com/"}).Draw(t, "url")
	path := rapid.StringMatching(`[a-z]{0,3}`).Draw(t, "path")
	desc := rapid.StringMatching(`[a-z<>&"' \]\t]{0,10}`).Draw(t, "desc")
	tags := rapid.StringMatching(`[a-z<>&" ]{0,8}`).Draw(t, "tags")
	feed := rapid.SampledFrom([]string{"", "https://a.com/feed", "http://b.org/rss?x=<1>&y", "c.net/atom.xml", "javascript:alert(2)"}).Draw(t, "feed")
	return line(u+path, desc, tags, feed)
}

// TestOutputsRoundTrip: links.txt parses back to the published links, and the
// feed gives each published link, newest first, with its time and tags.
func TestOutputsRoundTrip(t *testing.T) {
	s, h := newServer(t)
	rapid.Check(t, func(t *rapid.T) {
		text := ""
		for range rapid.IntRange(0, 3).Draw(t, "batches") {
			rows := rapid.SliceOf(rapid.Custom(genPublicRow)).Draw(t, "rows")
			when := time.Unix(rapid.Int64Range(0, 4102444800).Draw(t, "when"), 0)
			text, _, _, _ = addDated(text, rows, when)
		}
		write(t, s, text)
		want := published(text)

		got := parse(get(h, "/links/links.txt").Body.String())
		if len(got) != len(want) {
			t.Fatalf("links.txt has %d links, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i].URL != want[i].URL || got[i].Desc != want[i].Desc || !slices.Equal(got[i].Tags, want[i].Tags) || got[i].Feed != want[i].Feed {
				t.Fatalf("links.txt link %d is %+v, want %+v", i, got[i], want[i])
			}
		}

		f := feedOf(t, get(h, "/links/atom.xml"))
		if len(f.Entries) != len(want) {
			t.Fatalf("feed has %d entries, want %d", len(f.Entries), len(want))
		}
		for i, e := range f.Entries {
			l := want[len(want)-1-i]
			var tags []string
			for _, c := range e.Categories {
				tags = append(tags, c.Term)
			}
			if e.Title != l.Title() || e.Link.Href != l.URL || e.ID != l.URL || e.Updated != atomTime(l.Added) || !slices.Equal(tags, l.Tags) {
				t.Fatalf("entry %d is %+v, want %+v", i, e, l)
			}
		}
	})
}

func TestAtomReplacesCharactersThatXMLCannotHold(t *testing.T) {
	s, h := newServer(t)
	write(t, s, "https://a.com\tbell \x07 and ]]>\t\n")
	if f := feedOf(t, get(h, "/links/atom.xml")); f.Entries[0].Title != "bell � and ]]>" {
		t.Fatalf("title %q", f.Entries[0].Title)
	}
}

func TestNotModified(t *testing.T) {
	s, h := newServer(t)
	write(t, s, "https://a.com\tA\t\n")
	for _, path := range []string{"/links/links.txt", "/links/atom.xml", "/links/list.html"} {
		lm := get(h, path).Header().Get("Last-Modified")
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("If-Modified-Since", lm)
		if w := do(h, r, false); lm == "" || w.Code != http.StatusNotModified {
			t.Errorf("%s: Last-Modified %q, code %d", path, lm, w.Code)
		}
	}
}

func TestList(t *testing.T) {
	s, h := newServer(t)
	if body := get(h, "/links/list.html").Body.String(); body != "<p>No links yet.</p>\n" {
		t.Fatalf("empty list: %q", body)
	}
	write(t, s, "https://a.com/x/\t<A>\tx y\thttps://a.com/rss\nhttps://b.com\tB\t\nhttps://c.com\tC\ty y\n")
	want := `<p><a href="#x">x</a> <a href="#y">y</a> <a href="#untagged">untagged</a> </p>
<h2 id="x">x</h2>
<ul>
<li><a href="https://a.com/x/">&lt;A&gt;</a> <small>a.com/x</small> <a class="feed" href="https://a.com/rss">rss</a></li>
</ul>
<h2 id="y">y</h2>
<ul>
<li><a href="https://c.com">C</a> <small>c.com</small></li>
<li><a href="https://a.com/x/">&lt;A&gt;</a> <small>a.com/x</small> <a class="feed" href="https://a.com/rss">rss</a></li>
</ul>
<h2 id="untagged">untagged</h2>
<ul>
<li><a href="https://b.com">B</a> <small>b.com</small></li>
</ul>
`
	if body := get(h, "/links/list.html").Body.String(); body != want {
		t.Fatalf("list:\n%s\nwant:\n%s", body, want)
	}
}

func TestImport(t *testing.T) {
	s, h := newServer(t)
	write(t, s, "https://a.com\tA\t\n")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	f, _ := mw.CreateFormFile("file", "bookmarks.sbm")
	f.Write([]byte("https://a.com/\tagain\t\thttps://a.com/rss\nhttps://b.com\tB\tt\n"))
	mw.Close()
	r := httptest.NewRequest("POST", "/links/admin/import", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := do(h, r, true)
	if m := location(w); m != "Imported 1 links. Skipped 1 duplicates. Added 1 feeds to links that had none." {
		t.Fatalf("import: %d %q", w.Code, m)
	}
	// The link a.com keeps its description, and gets the feed.
	b, _ := os.ReadFile(s.links.path)
	if string(b) != "https://a.com\tA\t\thttps://a.com/rss\n"+dayLine+"https://b.com\tB\tt\n" {
		t.Fatalf("file: %q", b)
	}
}

func TestFeedCommand(t *testing.T) {
	s, _ := newServer(t)
	write(t, s, "https://a.com\tA\tx\thttps://github.com/a/commits.atom\n")
	for _, c := range []struct{ u, feed, message, file string }{
		// A feed that the link has already changes.
		{"a.com/", "https://a.com/Japanese.xml", "Saved the feed.", "https://a.com\tA\tx\thttps://a.com/Japanese.xml\n"},
		{"https://b.com", "https://b.com/rss", "No link has that URL.", "https://a.com\tA\tx\thttps://a.com/Japanese.xml\n"},
		{"https://a.com", "file:///x", "Give an http or https address for the feed.", "https://a.com\tA\tx\thttps://a.com/Japanese.xml\n"},
	} {
		m, err := saveFeed(s.links, c.u, c.feed)
		b, _ := os.ReadFile(s.links.path)
		if err != nil || m != c.message || string(b) != c.file {
			t.Errorf("feed %s %s: %q %v, file %q", c.u, c.feed, m, err, b)
		}
	}
}

func TestMergeCommand(t *testing.T) {
	s, _ := newServer(t)
	write(t, s, "https://a.com\tA\t\n")
	m, err := mergeFrom(s.links, strings.NewReader("https://b.com\tB\tt\thttps://b.com/rss\nhttps://a.com\tagain\t\n"), day)
	if err != nil || m != "Imported 1 links. Skipped 1 duplicates." {
		t.Fatalf("%q %v", m, err)
	}
	b, _ := os.ReadFile(s.links.path)
	if string(b) != "https://a.com\tA\t\n"+dayLine+"https://b.com\tB\tt\thttps://b.com/rss\n" {
		t.Fatalf("file: %q", b)
	}
	// The service must be able to read a file that root wrote.
	if st, err := os.Stat(s.links.path); runtime.GOOS != "windows" && (err != nil || st.Mode().Perm() != 0o644) {
		t.Fatalf("mode %v %v", st.Mode(), err)
	}
}

func TestCrossOriginPostRefused(t *testing.T) {
	s, h := newServer(t)
	r := form("/links/admin/add", url.Values{"url": {"https://evil.com"}})
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if w := do(h, r, true); w.Code != http.StatusForbidden {
		t.Fatalf("code %d", w.Code)
	}
	if _, err := os.Stat(s.links.path); err == nil {
		t.Fatal("cross-site POST wrote the file")
	}
}
