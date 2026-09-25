// sbm-webpublish publishes an sbm bookmark file as a list of links on a
// web site. The site gets the links as JSON from /links/links.json. The
// owner adds, deletes and imports links on /links/admin.
package main

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
)

//go:embed admin.html
var adminHTML string

var adminTmpl = template.Must(template.New("admin").Parse(adminHTML))

// maxImport is the largest file that the import form takes.
const maxImport = 10 << 20

// store keeps the links file. One mutex puts the changes in a sequence.
type store struct {
	mu   sync.Mutex
	path string
}

func (s *store) read() (string, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// change reads the file, gives its text to f, and writes the result. It
// writes a temporary file and renames it, so that a reader never sees half
// a file.
func (s *store) change(f func(string) string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, err := s.read()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".links-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(f(text)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

type server struct {
	links    *store
	password string
}

// jsonLink is one link in links.json.
type jsonLink struct {
	URL   string   `json:"url"`
	Title string   `json:"title"`
	Host  string   `json:"host"`
	Tags  []string `json:"tags"`
}

// publicLinks gives the links that the page can show: only the links with
// an http or https address. The newest link is first.
func publicLinks(text string) []jsonLink {
	out := []jsonLink{}
	for _, l := range parse(text) {
		if h := l.Href(); h != "" {
			tags := l.Tags
			if tags == nil {
				tags = []string{}
			}
			out = append([]jsonLink{{h, l.Title(), l.Host(), tags}}, out...)
		}
	}
	return out
}

func (s *server) linksJSON(w http.ResponseWriter, r *http.Request) {
	text, err := s.links.read()
	if err != nil {
		http.Error(w, "cannot read the links", http.StatusInternalServerError)
		log.Print(err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=60")
	json.NewEncoder(w).Encode(publicLinks(text))
}

// admin lets only the owner through: HTTP basic auth with any user name
// and the password of ADMIN_PASSWORD.
func (s *server) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, pw, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pw), []byte(s.password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="sbm-webpublish", charset="UTF-8"`)
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

type adminData struct {
	Links   []Link
	Message string
}

func (s *server) show(w http.ResponseWriter, message string) {
	text, err := s.links.read()
	if err != nil {
		http.Error(w, "cannot read the links", http.StatusInternalServerError)
		log.Print(err)
		return
	}
	ls := parse(text)
	slices.Reverse(ls)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTmpl.Execute(w, adminData{ls, message}); err != nil {
		log.Print(err)
	}
}

func (s *server) adminPage(w http.ResponseWriter, r *http.Request) {
	s.show(w, r.URL.Query().Get("m"))
}

// done sends the browser back to the admin page with a message.
func done(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "../admin?m="+template.URLQueryEscaper(message), http.StatusSeeOther)
}

func (s *server) add(w http.ResponseWriter, r *http.Request) {
	row := line(r.FormValue("url"), r.FormValue("desc"), r.FormValue("tags"))
	if row == "" {
		done(w, r, "Give a URL.")
		return
	}
	var added int
	err := s.links.change(func(text string) string {
		text, added, _ = merge(text, []string{row})
		return text
	})
	switch {
	case err != nil:
		log.Print(err)
		http.Error(w, "cannot write the links", http.StatusInternalServerError)
	case added == 0:
		done(w, r, "That URL is a link already.")
	default:
		done(w, r, "Added.")
	}
}

func (s *server) delete(w http.ResponseWriter, r *http.Request) {
	k := norm(r.FormValue("url"))
	if err := s.links.change(func(text string) string { return remove(text, k) }); err != nil {
		log.Print(err)
		http.Error(w, "cannot write the links", http.StatusInternalServerError)
		return
	}
	done(w, r, "Deleted.")
}

func (s *server) importFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImport)
	f, _, err := r.FormFile("file")
	if err != nil {
		done(w, r, "Choose a file of 10 MB or less.")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		done(w, r, "Cannot read the file.")
		return
	}
	var added, skipped int
	err = s.links.change(func(text string) string {
		text, added, skipped = merge(text, importRows(string(b)))
		return text
	})
	if err != nil {
		log.Print(err)
		http.Error(w, "cannot write the links", http.StatusInternalServerError)
		return
	}
	done(w, r, "Imported "+strconv.Itoa(added)+" links. Skipped "+strconv.Itoa(skipped)+" duplicates.")
}

// routes gives the handler of the server. The paths start with /links/,
// so that nginx can send /links/ here and serve the rest of the site.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /links/links.json", s.linksJSON)
	mux.HandleFunc("GET /links/admin", s.admin(s.adminPage))
	mux.HandleFunc("POST /links/admin/add", s.admin(s.add))
	mux.HandleFunc("POST /links/admin/delete", s.admin(s.delete))
	mux.HandleFunc("POST /links/admin/import", s.admin(s.importFile))
	// The admin forms use basic auth, which the browser sends with each
	// request. CrossOriginProtection refuses a POST from another site.
	return http.NewCrossOriginProtection().Handler(mux)
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	pw := os.Getenv("ADMIN_PASSWORD")
	if pw == "" {
		log.Fatal("set ADMIN_PASSWORD")
	}
	s := &server{links: &store{path: env("LINKS_FILE", "links.sbm")}, password: pw}
	addr := env("ADDR", "127.0.0.1:8082")
	log.Printf("listening on %s, links file %s", addr, s.links.path)
	log.Fatal(http.ListenAndServe(addr, s.routes()))
}
