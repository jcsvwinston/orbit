// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What an action answers with (EXT-03).
//
// An action used to answer with a message and nothing else, so "export these
// invoices as PDF" and "open the reconciliation of this batch" had no
// contract to ride: an application could put a URL in Data and hope a
// screen did something with it. An action can now answer in one of three
// ways — a message, a page of the panel to go to, or a file to download —
// and each one is checked before anything is sent.
//
// Two of them are places where an action's answer could become something
// else. A redirect an operator's input can steer is an open redirect; a
// download that names a path is a way to read any file the process can.
// So a redirect is a path inside the panel or it is refused, and a download
// is the bytes the action produced — the panel never opens a file on its
// behalf.

// ActionDownload is a file an action hands the operator.
type ActionDownload struct {
	// Filename is the name the browser saves the file under. Only its last
	// element is sent ("reports/q3.pdf" is sent as "q3.pdf"); a name that
	// is empty, "." or "..", or holds a control character, is refused.
	Filename string
	// ContentType is the file's media type ("application/pdf",
	// "text/csv; charset=utf-8"). It is required: the panel does not guess
	// what a file is, and it tells the browser not to either.
	ContentType string
	// Body is the content. The panel reads it once, up to 32 MiB, and
	// closes it when it is an io.Closer. A body larger than that is refused
	// whole, never sent truncated: a file cut short is worse than no file,
	// because it looks like one.
	Body io.Reader
}

// actionDownloadMaxBytes is the most an action may hand back as a file. It
// is the panel's own upload ceiling: what an operator may send it is what it
// will send an operator. A var so a test can lower it.
var actionDownloadMaxBytes int64 = 32 << 20 // 32 MiB

// The kinds of answer, as the response and the audit entry name them.
const (
	actionAnswerMessage  = "message"
	actionAnswerRedirect = "redirect"
	actionAnswerDownload = "download"
)

// actionAnswer is an action's result, checked and ready to send.
type actionAnswer struct {
	kind string
	// redirect is the checked path, or — when it was refused — the target
	// the action gave, kept for the audit entry.
	redirect string
	// download is the file to send; on a refused download only its name
	// and type are set, for the audit entry.
	download *preparedDownload
}

// preparedDownload is a download whose name, type and size have been
// checked and whose bytes have been read.
type preparedDownload struct {
	filename    string
	contentType string
	content     []byte
}

// prepareActionAnswer checks what an action answered. It returns the kind of
// answer even when it refuses one, so the audit entry can say what the
// action tried to send.
func prepareActionAnswer(result ActionResult) (actionAnswer, error) {
	target := strings.TrimSpace(result.Redirect)
	if result.Download != nil {
		// The body is the action's, and it is closed whatever happens to
		// the answer: a refused download must not leak a file handle.
		defer closeActionBody(result.Download.Body)
		answer := actionAnswer{kind: actionAnswerDownload, download: &preparedDownload{
			filename: result.Download.Filename, contentType: result.Download.ContentType,
		}}
		if target != "" {
			answer.redirect = target
			return answer, errors.New("an action answers with a redirect or with a download, not both")
		}
		prepared, err := prepareDownload(*result.Download)
		if err != nil {
			return answer, err
		}
		answer.download = prepared
		return answer, nil
	}
	if target != "" {
		checked, err := panelRedirect(target)
		if err != nil {
			return actionAnswer{kind: actionAnswerRedirect, redirect: target}, err
		}
		return actionAnswer{kind: actionAnswerRedirect, redirect: checked}, nil
	}
	return actionAnswer{kind: actionAnswerMessage}, nil
}

// audit adds the answer to an action's audit entry: its kind and what it
// named — the page, or the file's name, type and size.
func (a actionAnswer) audit(recorded map[string]any) {
	recorded["result"] = a.kind
	if a.redirect != "" {
		recorded["redirect"] = a.redirect
	}
	if a.download != nil {
		file := map[string]any{
			"filename":     a.download.filename,
			"content_type": a.download.contentType,
		}
		if a.download.content != nil {
			file["bytes"] = len(a.download.content)
		}
		recorded["download"] = file
	}
}

func closeActionBody(body io.Reader) {
	if closer, ok := body.(io.Closer); ok {
		_ = closer.Close()
	}
}

// panelRedirect checks that a redirect is a path inside the panel and
// returns it as it will be sent. The rules are the ones a browser's URL
// parser needs to be held to, not the ones a reader would assume:
//
//   - it starts with exactly one "/": "//evil.example" is a URL with a
//     host, and "evil.example" is relative to wherever the operator is;
//   - it has no scheme, host or user ("https://…", "javascript:…");
//   - it holds no backslash, which browsers read as a slash ("/\evil");
//   - it holds no control character, which browsers strip before parsing;
//   - no segment is "." or "..", decoded or not, because the panel is
//     mounted under a prefix and "/../../elsewhere" climbs out of it.
func panelRedirect(raw string) (string, error) {
	target := strings.TrimSpace(raw)
	for _, r := range target {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("redirect %q holds a control character", raw)
		}
	}
	if strings.Contains(target, `\`) {
		return "", fmt.Errorf("redirect %q holds a backslash, which a browser reads as a slash", raw)
	}
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "", fmt.Errorf("redirect %q is not a path inside the panel: it must start with one \"/\", relative to the panel (\"/data-studio\", \"/x/reports\")", raw)
	}
	u, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("redirect %q is not a path: %v", raw, err)
	}
	if u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return "", fmt.Errorf("redirect %q is not a path inside the panel", raw)
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("redirect %q holds a %q segment, which climbs out of the page it names", raw, segment)
		}
	}
	return target, nil
}

// prepareDownload checks a download's name and type and reads its body, up
// to the ceiling.
func prepareDownload(d ActionDownload) (*preparedDownload, error) {
	name, err := downloadFilename(d.Filename)
	if err != nil {
		return nil, err
	}
	contentType, err := downloadContentType(d.ContentType)
	if err != nil {
		return nil, err
	}
	if d.Body == nil {
		return nil, fmt.Errorf("download %q has no Body", name)
	}
	// One byte past the ceiling is enough to know it is over, and nothing
	// past that is read.
	content, err := io.ReadAll(io.LimitReader(d.Body, actionDownloadMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading download %q failed: %v", name, err)
	}
	if int64(len(content)) > actionDownloadMaxBytes {
		return nil, fmt.Errorf("download %q is larger than the %d MiB the panel sends", name, actionDownloadMaxBytes>>20)
	}
	if content == nil {
		content = []byte{}
	}
	return &preparedDownload{filename: name, contentType: contentType, content: content}, nil
}

// downloadFilename is the name a download is saved under: the last element
// of what the action gave, without a control character in it.
func downloadFilename(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("download file name %q is not valid UTF-8", raw)
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("download file name %q holds a control character", raw)
		}
	}
	name := strings.ReplaceAll(raw, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("download file name %q names no file", raw)
	}
	return name, nil
}

// downloadContentType checks a download's declared media type and returns
// it in canonical form, so nothing but a media type reaches the header.
func downloadContentType(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("a download declares its ContentType; the panel does not guess what a file is")
	}
	mediaType, params, err := mime.ParseMediaType(raw)
	if err != nil || !strings.Contains(mediaType, "/") {
		return "", fmt.Errorf("download content type %q is not a media type", raw)
	}
	formatted := mime.FormatMediaType(mediaType, params)
	if formatted == "" {
		return "", fmt.Errorf("download content type %q is not a media type", raw)
	}
	return formatted, nil
}

// write sends the file. The headers say what it is, that it is to be saved
// and not shown, and that nothing in it is to be run: a download of HTML
// opened from the browser's downloads is still a file, not a page of the
// panel with the operator's session.
func (d *preparedDownload) write(w http.ResponseWriter) error {
	h := w.Header()
	h.Set("Content-Type", d.contentType)
	h.Set("Content-Disposition", contentDisposition(d.filename))
	h.Set("Content-Length", strconv.Itoa(len(d.content)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	// A write that fails here is a client that went away; the status is
	// already sent, so there is nobody left to tell.
	_, _ = w.Write(d.content)
	return nil
}

// contentDisposition is an attachment header that carries the name both
// ways browsers read it: an ASCII fallback in filename= and the exact name,
// percent-encoded as RFC 5987 says, in filename*=.
func contentDisposition(name string) string {
	return `attachment; filename="` + asciiFilename(name) + `"; filename*=UTF-8''` + rfc5987(name)
}

// asciiFilename replaces what a quoted filename= cannot carry safely — any
// non-ASCII rune, a quote, a backslash, a percent sign some browsers decode
// — with an underscore.
func asciiFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r >= 0x7f || r == '"' || r == '\\' || r == '%' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// rfc5987 percent-encodes every byte that is not an attr-char.
func rfc5987(name string) string {
	const attrChars = "!#$&+-.^_`|~"
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') || strings.IndexByte(attrChars, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}
