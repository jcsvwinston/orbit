package admin

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

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func TestNegotiateEncoding(t *testing.T) {
	both := []string{"br", "gzip"}
	cases := []struct {
		name    string
		fields  []string
		offered []string
		want    string
	}{
		{"no field: as it is", nil, both, ""},
		{"a browser's field: brotli first", []string{"gzip, deflate, br, zstd"}, both, "br"},
		{"gzip only", []string{"gzip"}, both, "gzip"},
		{"the old alias", []string{"x-gzip"}, both, "gzip"},
		{"brotli refused by q=0", []string{"br;q=0, gzip"}, both, "gzip"},
		{"both refused", []string{"br;q=0, gzip;q=0"}, both, ""},
		{"a higher q wins over the server's order", []string{"br;q=0.5, gzip;q=0.9"}, both, "gzip"},
		{"a tie goes to the server's order", []string{"gzip;q=0.8, br;q=0.8"}, both, "br"},
		{"the wildcard covers what is not named", []string{"*"}, both, "br"},
		{"the wildcard does not undo a refusal", []string{"br;q=0, *"}, both, "gzip"},
		{"the wildcard refused", []string{"*;q=0"}, both, ""},
		{"identity only", []string{"identity"}, both, ""},
		{"case and spaces", []string{" GZip ; Q=1 "}, both, "gzip"},
		{"several field lines", []string{"identity", "gzip"}, both, "gzip"},
		{"a q that is not a number refuses", []string{"br;q=high, gzip"}, both, "gzip"},
		{"only what the build wrote", []string{"br"}, []string{"gzip"}, ""},
		{"nothing written", []string{"br, gzip"}, nil, ""},
	}
	for _, c := range cases {
		if got := negotiateEncoding(c.fields, c.offered); got != c.want {
			t.Errorf("%s: negotiateEncoding(%q, %v) = %q, want %q", c.name, c.fields, c.offered, got, c.want)
		}
	}
}

func gzipped(t *testing.T, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return b.Bytes()
}

func TestServePrecompressed(t *testing.T) {
	script := []byte(strings.Repeat("console.log('orbit');\n", 200))
	fsys := fstest.MapFS{
		"assets/app.js":      {Data: script},
		"assets/app.js.gz":   {Data: gzipped(t, script)},
		"assets/app.js.br":   {Data: []byte("brotli-bytes")},
		"assets/plain.css":   {Data: []byte("body{}")},
		"assets/only.css":    {Data: []byte(strings.Repeat("a{b:c}", 300))},
		"assets/only.css.gz": {Data: gzipped(t, []byte(strings.Repeat("a{b:c}", 300)))},
	}
	serve := func(method, name, accept string, extra ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/"+name, nil)
		if accept != "" {
			r.Header.Set("Accept-Encoding", accept)
		}
		for i := 0; i+1 < len(extra); i += 2 {
			r.Header.Set(extra[i], extra[i+1])
		}
		w := httptest.NewRecorder()
		if !servePrecompressed(w, r, fsys, name) {
			t.Fatalf("%s %s was not served", method, name)
		}
		return w
	}

	t.Run("brotli to a browser, typed by the file it encodes", func(t *testing.T) {
		w := serve(http.MethodGet, "assets/app.js", "gzip, deflate, br")
		if got := w.Header().Get("Content-Encoding"); got != "br" {
			t.Fatalf("Content-Encoding %q, want br", got)
		}
		if w.Body.String() != "brotli-bytes" {
			t.Errorf("served %q, not the .br sibling", w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
			t.Errorf("Content-Type %q is not the script's", ct)
		}
		if w.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("Vary %q", w.Header().Get("Vary"))
		}
	})
	t.Run("gzip that decodes to the file", func(t *testing.T) {
		w := serve(http.MethodGet, "assets/app.js", "gzip")
		if got := w.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding %q, want gzip", got)
		}
		zr, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		decoded, _ := io.ReadAll(zr)
		if !bytes.Equal(decoded, script) {
			t.Error("the gzip body does not decode to the file")
		}
	})
	t.Run("as it is to a client that asks for nothing, still varying", func(t *testing.T) {
		w := serve(http.MethodGet, "assets/app.js", "")
		if got := w.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding %q to a client that did not ask", got)
		}
		if !bytes.Equal(w.Body.Bytes(), script) {
			t.Error("the identity body is not the file")
		}
		if w.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("the identity answer of a file with siblings must vary too; Vary %q", w.Header().Get("Vary"))
		}
	})
	t.Run("only the encodings the build wrote", func(t *testing.T) {
		w := serve(http.MethodGet, "assets/only.css", "br")
		if got := w.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding %q for an encoding the build did not write", got)
		}
		if w := serve(http.MethodGet, "assets/only.css", "br, gzip"); w.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("Content-Encoding %q, want gzip", w.Header().Get("Content-Encoding"))
		}
	})
	t.Run("a small file travels as it is and does not vary", func(t *testing.T) {
		w := serve(http.MethodGet, "assets/plain.css", "br, gzip")
		if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "" {
			t.Errorf("Content-Encoding %q Vary %q for a file with no sibling", w.Header().Get("Content-Encoding"), w.Header().Get("Vary"))
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
			t.Errorf("Content-Type %q", ct)
		}
	})
	t.Run("HEAD says the encoded length", func(t *testing.T) {
		w := serve(http.MethodHead, "assets/app.js", "br")
		if w.Header().Get("Content-Encoding") != "br" || w.Header().Get("Content-Length") != "12" || w.Body.Len() != 0 {
			t.Errorf("HEAD: encoding %q length %q body %d", w.Header().Get("Content-Encoding"), w.Header().Get("Content-Length"), w.Body.Len())
		}
	})
	t.Run("a range is answered with the whole representation", func(t *testing.T) {
		w := serve(http.MethodGet, "assets/app.js", "br", "Range", "bytes=0-5")
		if w.Code != http.StatusOK || w.Body.String() != "brotli-bytes" {
			t.Errorf("range: %d %q", w.Code, w.Body.String())
		}
	})
	t.Run("not a file: left to the caller", func(t *testing.T) {
		for _, name := range []string{"assets", "assets/missing.js", "", "../assets/app.js.gz/x"} {
			w := httptest.NewRecorder()
			if servePrecompressed(w, httptest.NewRequest(http.MethodGet, "/x", nil), fsys, name) {
				t.Errorf("%q was answered (%d)", name, w.Code)
			}
		}
	})
}

// The panel answers its own built files in the encoding the browser
// accepts. Through the panel's routes, over the dist the ui module embeds:
// when that dist carries the encodings (the ui module this tree builds),
// every file that has one is served in it; the dist of an older ui release
// carries none, and then every file is served as it is.
func TestPanel_ServesTheDistCompressed(t *testing.T) {
	t.Setenv(adminUIDirEnv, "")
	dist := embeddedDist(t)

	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	panel.config.Prefix = "/nucleus-admin"
	root := router.NewMux()
	root.Mount("/nucleus-admin", panel.Handler())
	srv := httptest.NewServer(root)
	defer srv.Close()
	// A client that says what it accepts and reads the bytes as sent.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}

	encoded := 0
	err := fs.WalkDir(dist, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".gz") || strings.HasSuffix(p, ".br") {
			return err
		}
		raw, _ := fs.ReadFile(dist, p)
		_, gzErr := fs.Stat(dist, p+".gz")
		for _, accept := range []string{"gzip, deflate, br", "gzip"} {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/nucleus-admin/"+p, nil)
			req.Header.Set("Accept-Encoding", accept)
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			got := res.Header.Get("Content-Encoding")
			switch {
			case gzErr != nil:
				if got != "" || !bytes.Equal(body, raw) {
					t.Errorf("%s (no encoding in the dist) answered Content-Encoding %q", p, got)
				}
			case accept == "gzip":
				if got != "gzip" {
					t.Errorf("%s to Accept-Encoding gzip: Content-Encoding %q", p, got)
					continue
				}
				zr, err := gzip.NewReader(bytes.NewReader(body))
				if err != nil {
					t.Errorf("%s: %v", p, err)
					continue
				}
				decoded, _ := io.ReadAll(zr)
				if !bytes.Equal(decoded, raw) {
					t.Errorf("%s: the gzip answer does not decode to the file", p)
				}
				if !strings.Contains(res.Header.Get("Vary"), "Accept-Encoding") {
					t.Errorf("%s: Vary %q", p, res.Header.Get("Vary"))
				}
				encoded++
			default:
				if got != "br" {
					t.Errorf("%s to a browser's Accept-Encoding: Content-Encoding %q, want br", p, got)
				}
				if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "font-src 'self';") {
					t.Errorf("%s: the policy's font-src changed: %q", p, csp)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if encoded == 0 {
		t.Log("the embedded dist carries no encodings (a ui release from before they were written): every file was served as it is")
	}
}

// An application that installs Nucleus's compression middleware in front of
// the panel does not get its files compressed twice: the middleware passes
// an answer that already carries Content-Encoding through.
func TestPanel_EncodedFilesPassNucleusCompress(t *testing.T) {
	t.Setenv(adminUIDirEnv, "")
	dist := embeddedDist(t)
	var name string
	_ = fs.WalkDir(dist, "assets", func(p string, d fs.DirEntry, err error) error {
		if err == nil && name == "" && strings.HasSuffix(p, ".js") {
			if _, err := fs.Stat(dist, p+".gz"); err == nil {
				name = p
			}
		}
		return err
	})
	if name == "" {
		t.Skip("the embedded dist carries no encodings (a ui release from before they were written)")
	}
	raw, _ := fs.ReadFile(dist, name)

	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	panel.config.Prefix = "/nucleus-admin"
	compressed := router.Compress(6)(panel.Handler())
	root := router.NewMux()
	root.Mount("/nucleus-admin", compressed)
	srv := httptest.NewServer(root)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/nucleus-admin/"+name, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding %q", got)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	once, _ := io.ReadAll(zr)
	if !bytes.Equal(once, raw) {
		t.Errorf("the body decoded once is not %s (%d bytes, want %d): compressed twice?", name, len(once), len(raw))
	}
}
