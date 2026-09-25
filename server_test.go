package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newServer(t *testing.T) (*server, http.Handler) {
	s := &server{links: &store{path: filepath.Join(t.TempDir(), "links.sbm")}, password: "pw"}
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

func form(path string, v url.Values) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func publicJSON(t *testing.T, h http.Handler) []jsonLink {
	w := do(h, httptest.NewRequest("GET", "/links/links.json", nil), false)
	var ls []jsonLink
	if err := json.Unmarshal(w.Body.Bytes(), &ls); err != nil {
		t.Fatal(err, w.Body.String())
	}
	return ls
}

func TestAdminNeedsPassword(t *testing.T) {
	_, h := newServer(t)
	for _, r := range []*http.Request{
		httptest.NewRequest("GET", "/links/admin", nil),
		form("/links/admin/add", url.Values{"url": {"https://a.com"}}),
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

func TestAddDeleteAndJSON(t *testing.T) {
	s, h := newServer(t)
	if ls := publicJSON(t, h); len(ls) != 0 {
		t.Fatalf("empty store gave %v", ls)
	}
	do(h, form("/links/admin/add", url.Values{"url": {"https://a.com"}, "desc": {"A"}, "tags": {"x y"}}), true)
	w := do(h, form("/links/admin/add", url.Values{"url": {"https://b.com"}}), true)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/links/admin?m=Added." {
		t.Fatalf("add: %d %q", w.Code, w.Header().Get("Location"))
	}
	w = do(h, form("/links/admin/add", url.Values{"url": {"http://www.a.com/"}}), true)
	if !strings.Contains(w.Header().Get("Location"), "already") {
		t.Fatalf("duplicate add: %q", w.Header().Get("Location"))
	}
	ls := publicJSON(t, h)
	if len(ls) != 2 || ls[0].URL != "https://b.com" || ls[1].Title != "A" || len(ls[1].Tags) != 2 {
		t.Fatalf("json: %+v", ls)
	}
	do(h, form("/links/admin/delete", url.Values{"url": {"https://b.com"}}), true)
	b, _ := os.ReadFile(s.links.path)
	if string(b) != "https://a.com\tA\tx y\n" {
		t.Fatalf("file: %q", b)
	}
}

func TestJSONHidesUnsafeLinks(t *testing.T) {
	s, h := newServer(t)
	os.WriteFile(s.links.path, []byte("javascript:alert(1)\tx\t\nhttps://a.com\tA\t\n"), 0o644)
	if ls := publicJSON(t, h); len(ls) != 1 || ls[0].URL != "https://a.com" {
		t.Fatalf("json: %+v", ls)
	}
}

func TestImport(t *testing.T) {
	s, h := newServer(t)
	os.WriteFile(s.links.path, []byte("https://a.com\tA\t\n"), 0o644)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	f, _ := mw.CreateFormFile("file", "bookmarks.sbm")
	f.Write([]byte("https://a.com/\tagain\t\nhttps://b.com\tB\tt\n"))
	mw.Close()
	r := httptest.NewRequest("POST", "/links/admin/import", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := do(h, r, true)
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "Imported+1+links.+Skipped+1") {
		t.Fatalf("import: %d %q", w.Code, loc)
	}
	b, _ := os.ReadFile(s.links.path)
	if string(b) != "https://a.com\tA\t\nhttps://b.com\tB\tt\n" {
		t.Fatalf("file: %q", b)
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
