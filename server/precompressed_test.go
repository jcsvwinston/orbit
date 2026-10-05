package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	orbitui "github.com/jcsvwinston/orbit/ui"
)

func gzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return b.Bytes()
}

// The fleet's SPA handler serves a file in the encoding the browser
// accepts, the document without caching, and the document for a path that
// is a screen of the SPA rather than a file.
func TestSPAHandler_ServesPrecompressed(t *testing.T) {
	script := []byte(strings.Repeat("export const fleet = 1;\n", 100))
	index := []byte("<!doctype html><title>fleet</title>")
	h := spaHandler(fstest.MapFS{
		"index.html":           {Data: index},
		"assets/index-a.js":    {Data: script},
		"assets/index-a.js.gz": {Data: gzipBytes(t, script)},
		"assets/index-a.js.br": {Data: []byte("br")},
	})
	get := func(target, accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		if accept != "" {
			r.Header.Set("Accept-Encoding", accept)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := get("/assets/index-a.js", "gzip, deflate, br")
	if w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "br" || w.Body.String() != "br" {
		t.Errorf("a browser got %d, Content-Encoding %q", w.Code, w.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "javascript") || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("Content-Type %q Vary %q", w.Header().Get("Content-Type"), w.Header().Get("Vary"))
	}
	w = get("/assets/index-a.js", "")
	if w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), script) {
		t.Errorf("a client that asked for nothing got Content-Encoding %q", w.Header().Get("Content-Encoding"))
	}
	for _, target := range []string{"/", "/index.html", "/nodes"} {
		w = get(target, "gzip")
		if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), index) {
			t.Errorf("%s: %d %q", target, w.Code, w.Body.String())
		}
		if target != "/nodes" && w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control %q", target, w.Header().Get("Cache-Control"))
		}
	}
}

// Through the UI listener's handler, over the fleet entry the ui module
// embeds: a file the build encoded is served encoded, and decodes to the
// file. The dist of a ui release from before the encodings were written
// carries none, and then everything is served as it is.
func TestStaticUIHandler_ServesTheFleetCompressed(t *testing.T) {
	dist := orbitui.Fleet()
	if dist == nil {
		t.Skip("the ui module embeds no fleet entry")
	}
	h := staticUIHandler()
	encoded := 0
	err := fs.WalkDir(dist, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".gz") || strings.HasSuffix(p, ".br") {
			return err
		}
		raw, _ := fs.ReadFile(dist, p)
		r := httptest.NewRequest(http.MethodGet, "/"+p, nil)
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if _, err := fs.Stat(dist, p+".gz"); err != nil {
			if w.Header().Get("Content-Encoding") != "" {
				t.Errorf("%s has no encoding and was answered with %q", p, w.Header().Get("Content-Encoding"))
			}
			return nil
		}
		if w.Header().Get("Content-Encoding") != "gzip" {
			t.Errorf("%s: Content-Encoding %q, want gzip", p, w.Header().Get("Content-Encoding"))
			return nil
		}
		zr, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		decoded, _ := io.ReadAll(zr)
		if !bytes.Equal(decoded, raw) {
			t.Errorf("%s: the gzip answer does not decode to the file", p)
		}
		encoded++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if encoded == 0 {
		t.Log("the embedded fleet entry carries no encodings (a ui release from before they were written)")
	}
}
