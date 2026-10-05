package server

// The admin server's built files travel compressed. The ui module's build
// writes, beside every text file of at least 1 KiB, the same bytes encoded
// twice — <file>.br and <file>.gz (ui/tools/precompress.ts) — and the server
// answers a request with the one the browser accepts, or with the file as
// it is. Nothing is compressed per request.
//
// The in-process panel serves its entry with a copy of this file
// (internal/admin/precompressed.go). The two modules share no package, and
// putting this in the one module both require, ui, would leave the panel
// and the server unable to compile against the ui release they pin until
// the next one is cut (ADR-006). Everything from the import block down is
// the same text in both files, and internal/fleettest fails when they
// drift.

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// precompressedEncodings are the encodings a build writes, in the order the
// server prefers them when the client accepts both equally.
var precompressedEncodings = []struct{ token, ext string }{
	{"br", ".br"},
	{"gzip", ".gz"},
}

// servePrecompressed answers r with the file name of fsys, in the encoding
// the client accepts among those the build wrote beside it, and reports
// whether it did. It answers nothing (false) when name is not a regular
// file, so the caller keeps its own not-found or fallback. Every response
// for a file that has an encoded sibling varies on Accept-Encoding, the
// identity one too: a shared cache must not hand the encoded bytes to a
// client that did not ask for them, nor the plain ones to everybody.
func servePrecompressed(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" || !fs.ValidPath(name) || !isRegularFile(fsys, name) {
		return false
	}
	var offered []string
	for _, enc := range precompressedEncodings {
		if isRegularFile(fsys, name+enc.ext) {
			offered = append(offered, enc.token)
		}
	}
	h := w.Header()
	if len(offered) > 0 {
		h.Add("Vary", "Accept-Encoding")
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	h.Set("Content-Type", ctype)

	file := name
	if chosen := negotiateEncoding(r.Header.Values("Accept-Encoding"), offered); chosen != "" {
		for _, enc := range precompressedEncodings {
			if enc.token == chosen {
				file = name + enc.ext
			}
		}
		h.Set("Content-Encoding", chosen)
	}
	// The file is written whole, with its length: the names are hashed and
	// nothing asks the panel for part of one, so a Range is answered with
	// the whole representation (RFC 9110 lets a server ignore it), and
	// http.ServeContent is not used because it drops Content-Length from
	// an encoded answer.
	content, err := fs.ReadFile(fsys, file)
	if err != nil {
		h.Del("Content-Encoding")
		http.Error(w, "read "+name, http.StatusInternalServerError)
		return true
	}
	h.Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(content)
	}
	return true
}

func isRegularFile(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}

// negotiateEncoding picks, among offered, the content coding the
// Accept-Encoding field values prefer (RFC 9110 §12.5.3): the highest
// q-value above zero, the server's order on a tie. "*" stands for every
// coding the field does not name, "x-gzip" for "gzip". A request without
// the field, or that accepts none of offered, gets the file as it is ("").
func negotiateEncoding(fields []string, offered []string) string {
	if len(offered) == 0 {
		return ""
	}
	weights := map[string]float64{}
	wildcard := -1.0
	for _, field := range fields {
		for _, element := range strings.Split(field, ",") {
			coding, params, _ := strings.Cut(strings.TrimSpace(element), ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			if coding == "" {
				continue
			}
			if coding == "x-gzip" {
				coding = "gzip"
			}
			q := 1.0
			for _, param := range strings.Split(params, ";") {
				key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
					continue
				}
				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || parsed < 0 || parsed > 1 {
					parsed = 0
				}
				q = parsed
			}
			if coding == "*" {
				wildcard = q
				continue
			}
			weights[coding] = q
		}
	}
	best, bestQ := "", 0.0
	for _, coding := range offered {
		q, named := weights[coding]
		if !named {
			if wildcard < 0 {
				continue
			}
			q = wildcard
		}
		if q > bestQ {
			best, bestQ = coding, q
		}
	}
	return best
}

// precompressedOr serves the file the request names below prefix from fsys
// with servePrecompressed, and hands anything that is not a regular file to
// fallback (with the prefix stripped, as http.StripPrefix would).
func precompressedOr(fsys fs.FS, prefix string, fallback http.Handler) http.HandlerFunc {
	stripped := http.StripPrefix(prefix, fallback)
	return func(w http.ResponseWriter, r *http.Request) {
		if name, ok := strings.CutPrefix(r.URL.Path, prefix); ok && servePrecompressed(w, r, fsys, name) {
			return
		}
		stripped.ServeHTTP(w, r)
	}
}
