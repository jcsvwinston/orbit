package admin

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const adminUIDirEnv = "NUCLEUS_ADMIN_UI_DIR"

//go:embed ui_fallback/*
var fallbackUIFS embed.FS

// builtUIFS is the real admin SPA, embedded from internal/admin/ui/dist at build
// time. orbit commits the built dist so a consumer that mounts the module gets
// the full admin out of the box — no separate asset deployment (ADR-019: the
// admin ships as a normal Go dependency, closing fleetdesk finding #9). The
// `all:` prefix includes asset files whose names begin with `_` or `.`.
//
//go:embed all:ui/dist
var builtUIFS embed.FS

// adminUIContentFS resolves the admin SPA filesystem, in order:
//  1. NUCLEUS_ADMIN_UI_DIR — a dev override pointing at a built dist on disk;
//  2. the SPA embedded in the binary (the shipped distribution);
//  3. the placeholder (only if the embedded dist is somehow absent).
func adminUIContentFS() fs.FS {
	if dir := strings.TrimSpace(os.Getenv(adminUIDirEnv)); dir != "" && adminUIBuildDirUsable(dir) {
		return os.DirFS(dir)
	}
	if sub, err := fs.Sub(builtUIFS, "ui/dist"); err == nil && adminUIFSHasIndex(sub) {
		return sub
	}
	if fsys, err := fs.Sub(fallbackUIFS, "ui_fallback"); err == nil {
		return fsys
	}
	return os.DirFS(".")
}

// adminUIFSHasIndex reports whether fsys contains a usable index.html entrypoint.
func adminUIFSHasIndex(fsys fs.FS) bool {
	info, err := fs.Stat(fsys, "index.html")
	return err == nil && !info.IsDir()
}

func adminUIBuildDirUsable(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !info.IsDir()
}

// injectHeadMeta inserts a meta tag right after the document's opening
// <head>. When the document has no <head> (not the case for real SPA
// builds), a closed synthetic head is prepended so the output stays valid.
func injectHeadMeta(content []byte, name, value string) []byte {
	meta := fmt.Sprintf(`<meta name="%s" content="%s">`, html.EscapeString(name), html.EscapeString(value))
	contentStr := string(content)
	if strings.Contains(contentStr, "<head>") {
		return []byte(strings.Replace(contentStr, "<head>", "<head>"+meta, 1))
	}
	return []byte("<head>" + meta + "</head>\n" + contentStr)
}

func injectAdminPrefix(content []byte, prefix string) []byte {
	return injectHeadMeta(content, "nucleus-admin-prefix", NormalizePrefix(prefix))
}

// injectLoginMessage surfaces a login error/info message to the admin SPA as
// a meta tag — the same mechanism as the prefix injection — so the SPA login
// page can render feedback when a POST re-serves it (e.g. rejected
// credentials). Without it the SPA path silently dropped the message and a
// failed login was indistinguishable from "nothing happened". Empty messages
// inject nothing; an error wins over an info message.
func injectLoginMessage(content []byte, errorMsg, infoMsg string) []byte {
	name, msg := "nucleus-admin-login-info", infoMsg
	if errorMsg != "" {
		name, msg = "nucleus-admin-login-error", errorMsg
	}
	if msg == "" {
		return content
	}
	return injectHeadMeta(content, name, msg)
}
