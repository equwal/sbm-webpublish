package main

// The links file is an sbm bookmark file. Each line is one link:
//
//	URL<tab>description<tab>tag tag tag
//
// The parse, import and duplicate rules are the rules of bm and sbm-sync,
// so a file from bm works here, and the file here works in bm.
//
// A feed needs the time of each link, and a bm line has no time. So the
// admin page writes a date line above the links that it adds:
//
//	# 2026-09-25T08:00:00Z
//
// bm ignores each line that starts with "#", so the file stays an sbm file.

import (
	"html"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	scheme   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	notTag   = regexp.MustCompile(`[^a-z0-9]+`)
	imported = regexp.MustCompile(`^(https?|ftp|file)://`)
)

// Link is one link of the file. Added is the time of the last date line
// above the link, or zero when there is no date line above it.
type Link struct {
	URL, Desc string
	Tags      []string
	Added     time.Time
}

// Title gives the text of the link.
func (l Link) Title() string {
	if strings.TrimSpace(l.Desc) == "" {
		return l.URL
	}
	return l.Desc
}

// Host gives the host of the URL.
func (l Link) Host() string {
	u := scheme.ReplaceAllString(l.URL, "")
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return u
}

// Href gives the address for the link on the page. Only http and https
// addresses come out, so that a line of the file cannot run script in the
// page. An address without a scheme gets https. Other schemes give "".
func (l Link) Href() string {
	s := strings.ToLower(l.URL)
	switch {
	case strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://"):
		return l.URL
	case !strings.Contains(l.URL, ":"):
		return "https://" + l.URL
	}
	return ""
}

// lines splits a file into lines without their ends. CRLF counts as LF.
func lines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// join is the reverse of lines: each line ends with LF.
func join(ls []string) string {
	if len(ls) == 0 {
		return ""
	}
	return strings.Join(ls, "\n") + "\n"
}

// norm gives the form of a URL that bm uses to find duplicates: two URLs are
// the same link when they differ only in scheme, a leading "www.", trailing
// slashes or the case of the host.
func norm(u string) string {
	u = scheme.ReplaceAllString(u, "")
	host, rest := u, ""
	if i := strings.IndexByte(u, '/'); i >= 0 {
		host, rest = u[:i], u[i:]
	}
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	return host + strings.TrimRight(rest, "/")
}

// key gives the normal form of the URL of a line. Comments and empty lines
// give "".
func key(line string) string {
	if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	u, _, _ := strings.Cut(line, "\t")
	if !strings.Contains(line, "\t") {
		u = strings.Fields(line)[0]
	}
	return norm(strings.TrimSpace(u))
}

// parseLine gives the link of a line, and false for a comment or an empty
// line. A line without a tab has the old format "URL description". Its
// first word is the URL.
func parseLine(line string) (Link, bool) {
	if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
		return Link{}, false
	}
	fields := strings.Split(line, "\t")
	if len(fields) == 1 {
		words := strings.Fields(line)
		return Link{URL: words[0], Desc: strings.Join(words[1:], " ")}, true
	}
	l := Link{URL: strings.TrimSpace(fields[0]), Desc: fields[1]}
	if len(fields) > 2 {
		l.Tags = strings.Fields(fields[2])
	}
	return l, l.URL != ""
}

// dateLine gives the date line of t.
func dateLine(t time.Time) string {
	return "# " + t.UTC().Format(time.RFC3339)
}

// dateOf gives the time of a date line, and false for all other lines.
func dateOf(line string) (time.Time, bool) {
	s, ok := strings.CutPrefix(line, "#")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	return t, err == nil
}

// parse gives the links of a file, in the order of the file.
func parse(text string) []Link {
	var out []Link
	var added time.Time
	for _, s := range lines(text) {
		if t, ok := dateOf(s); ok {
			added = t
		} else if l, ok := parseLine(s); ok {
			l.Added = added
			out = append(out, l)
		}
	}
	return out
}

// line gives the file line of a link, as bm writes it: the URL without
// white space, the description without tabs and line breaks, and the tags
// with one space between them. It gives "" when the URL is empty or starts
// with "#".
func line(url, desc, tags string) string {
	u := strings.Join(strings.Fields(url), "")
	if u == "" || strings.HasPrefix(u, "#") {
		return ""
	}
	d := strings.TrimSpace(strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(desc))
	return u + "\t" + d + "\t" + strings.Join(strings.Fields(tags), " ")
}

// importRows gives the link lines of an imported file: the Netscape HTML
// that browsers export, or an sbm file.
func importRows(text string) []string {
	text = strings.TrimPrefix(text, string(rune(0xFEFF)))
	if strings.HasPrefix(strings.TrimSpace(text), "<") {
		return fromHTML(text)
	}
	return lines(text)
}

// fromHTML gives the link lines of the Netscape HTML that browsers export,
// as bm-import does: <H3> opens a folder, </DL> closes it, and the folders
// of a link become its tags. Only http, https, ftp and file URLs stay.
func fromHTML(text string) []string {
	var folders, out []string
	for _, s := range lines(text) {
		upper := upperASCII(s)
		if at := strings.Index(upper, "<H3"); at >= 0 {
			name := s[at:]
			name = name[strings.IndexByte(name, '>')+1:]
			if end := strings.Index(upperASCII(name), "</H3"); end >= 0 {
				name = name[:end]
			}
			folders = append(folders, folderTag(htmlText(name)))
		}
		if at := strings.Index(upper, `HREF="`); at >= 0 {
			u, rest, ok := strings.Cut(s[at+len(`HREF="`):], `"`)
			name := rest[strings.IndexByte(rest, '>')+1:]
			if end := strings.Index(upperASCII(name), "</A"); end >= 0 {
				name = name[:end]
			}
			var tags []string
			for _, f := range folders {
				if f != "" && !slices.Contains(tags, f) {
					tags = append(tags, f)
				}
			}
			if u = htmlText(u); ok && imported.MatchString(u) {
				out = append(out, u+"\t"+htmlText(name)+"\t"+strings.Join(tags, " "))
			}
		}
		if strings.Contains(upper, "</DL>") && len(folders) > 0 {
			folders = folders[:len(folders)-1]
		}
	}
	return out
}

// upperASCII gives s with the letters a to z in upper case. Each byte keeps
// its index, so an index in the result is an index in s.
func upperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'a' <= c && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// htmlText gives the text of HTML without its entities, with one space in
// place of each run of white space.
func htmlText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// folderTag gives the tag of a folder name: "Dev Tools" is dev-tools.
func folderTag(name string) string {
	return strings.Trim(notTag.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// merge adds to text the rows whose URL is not in text and not in a row
// before them, as bm --merge does. It gives the new text, the number of rows
// that it added, and the number that it skipped as duplicates.
func merge(text string, rows []string) (string, int, int) {
	ls := lines(text)
	seen := map[string]bool{}
	for _, s := range ls {
		seen[key(s)] = true
	}
	added, skipped := 0, 0
	for _, row := range rows {
		k := key(row)
		switch {
		case k == "":
		case seen[k]:
			skipped++
		default:
			seen[k] = true
			f := append(strings.SplitN(row, "\t", 4), "", "")
			if !strings.Contains(row, "\t") {
				words := strings.Fields(row)
				f = []string{words[0], strings.Join(words[1:], " "), ""}
			}
			ls = append(ls, line(f[0], f[1], f[2]))
			added++
		}
	}
	return join(ls), added, skipped
}

// addDated adds rows to text as merge does, below a date line of t. It
// writes the date line only when it adds a row.
func addDated(text string, rows []string, t time.Time) (string, int, int) {
	out, added, skipped := merge(join(append(lines(text), dateLine(t))), rows)
	if added == 0 {
		return text, 0, skipped
	}
	return out, added, skipped
}

// remove gives text without the lines whose URL has the normal form k.
func remove(text, k string) string {
	var out []string
	for _, s := range lines(text) {
		if k == "" || key(s) != k {
			out = append(out, s)
		}
	}
	return join(out)
}
