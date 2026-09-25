package main

// The public outputs. The server makes each one from the links file at each
// request, and puts in only the links with an http or https address:
//
//	/links/links.txt   the links as sbm lines, for cut, awk, dmenu and bm
//	/links/atom.xml    an Atom feed, for sfeed and other feed readers
//	/links/list.html   the links as HTML, for a page of the site to include

import (
	"bytes"
	"encoding/xml"
	"html/template"
	"log"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"
)

// published gives the links of text that the outputs show, in the order of
// the file. The URL of each link is the address that Href gives, and the
// feed is the address that FeedHref gives.
func published(text string) []Link {
	var out []Link
	for _, l := range parse(text) {
		if l.URL, l.Feed = l.Href(), l.FeedHref(); l.URL != "" {
			out = append(out, l)
		}
	}
	return out
}

// serve sends the output that render makes from the links. The time of the
// file is the Last-Modified time, so that a client can send
// If-Modified-Since and get 304 Not Modified.
func (s *server) serve(w http.ResponseWriter, r *http.Request, contentType string, render func([]Link, time.Time) []byte) {
	text, mod, err := s.links.read()
	if err != nil {
		log.Print(err)
		http.Error(w, "cannot read the links", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, "", mod, bytes.NewReader(render(published(text), mod)))
}

// plain sends the links as sbm lines, oldest first: URL, description, tags
// and feed, with a tab between them. A link without a feed has no fourth
// field.
func (s *server) plain(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, "text/plain; charset=utf-8", func(ls []Link, _ time.Time) []byte {
		var b strings.Builder
		for _, l := range ls {
			b.WriteString(line(l.URL, l.Desc, strings.Join(l.Tags, " "), l.Feed) + "\n")
		}
		return []byte(b.String())
	})
}

// The Atom feed (RFC 4287).
type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Link    atomLink    `xml:"link"`
	Updated string      `xml:"updated"`
	Author  string      `xml:"author>name"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title      string         `xml:"title"`
	Link       atomLink       `xml:"link"`
	ID         string         `xml:"id"`
	Updated    string         `xml:"updated"`
	Categories []atomCategory `xml:"category"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Href string `xml:"href,attr"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

func atomTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// proto gives the scheme that the client used. Behind nginx,
// X-Forwarded-Proto gives it.
func proto(r *http.Request) string {
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}

// feed sends the links as an Atom feed, newest first. The time of a link is
// the time of its date line. A link without a date line gets the time of the
// file. Each tag is a category. The feed gets its address and name from the
// request, so it needs no settings.
func (s *server) feed(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, "application/atom+xml; charset=utf-8", func(ls []Link, mod time.Time) []byte {
		self := proto(r) + "://" + r.Host + r.URL.Path
		f := atomFeed{
			Title:   r.Host + " links",
			ID:      self,
			Link:    atomLink{Rel: "self", Href: self},
			Updated: atomTime(mod),
			Author:  r.Host,
		}
		for _, l := range slices.Backward(ls) {
			t := l.Added
			if t.IsZero() {
				t = mod
			}
			e := atomEntry{Title: l.Title(), Link: atomLink{Href: l.URL}, ID: l.URL, Updated: atomTime(t)}
			for _, tag := range l.Tags {
				e.Categories = append(e.Categories, atomCategory{tag})
			}
			f.Entries = append(f.Entries, e)
		}
		// Marshal fails only for types that XML cannot hold, and the feed
		// has only strings. It writes U+FFFD for each character that XML
		// cannot hold.
		b, _ := xml.MarshalIndent(f, "", "\t")
		return append([]byte(xml.Header), append(b, '\n')...)
	})
}

// group is the links of one tag, for list.html.
type group struct {
	Tag   string
	Links []Link
}

// groups puts each link under each of its tags, newest link first. The tags
// are in alphabetical order. The links without tags come last, under
// "untagged".
func groups(ls []Link) []group {
	byTag := map[string][]Link{}
	for _, l := range slices.Backward(ls) {
		tags := l.Tags
		if len(tags) == 0 {
			tags = []string{""}
		}
		for _, t := range slices.Compact(slices.Sorted(slices.Values(tags))) {
			byTag[t] = append(byTag[t], l)
		}
	}
	var out []group
	for _, t := range slices.Sorted(maps.Keys(byTag)) {
		if t != "" {
			out = append(out, group{t, byTag[t]})
		}
	}
	if untagged := byTag[""]; untagged != nil {
		out = append(out, group{"untagged", untagged})
	}
	return out
}

var listTmpl = template.Must(template.New("list").Parse(`{{with .}}<p>{{range .}}<a href="#{{.Tag}}">{{.Tag}}</a> {{end}}</p>
{{range .}}<h2 id="{{.Tag}}">{{.Tag}}</h2>
<ul>
{{range .Links}}<li><a href="{{.URL}}">{{.Title}}</a> <small>{{.Address}}</small>{{with .Feed}} <a class="feed" href="{{.}}">rss</a>{{end}}</li>
{{end}}</ul>
{{end}}{{else}}<p>No links yet.</p>
{{end}}`))

// list sends the links as an HTML fragment, under each of their tags. A link
// with a feed has an "rss" link to the feed after it. A page of the site
// includes the fragment with nginx SSI, so the page needs no script:
//
//	<!--# include virtual="/links/list.html" -->
func (s *server) list(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, "text/html; charset=utf-8", func(ls []Link, _ time.Time) []byte {
		var b bytes.Buffer
		if err := listTmpl.Execute(&b, groups(ls)); err != nil {
			log.Print(err)
		}
		return b.Bytes()
	})
}
