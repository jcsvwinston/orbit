package admin

import (
	"embed"

	"fmt"
	orbitui "github.com/jcsvwinston/orbit/ui"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const adminUIDirEnv = "NUCLEUS_ADMIN_UI_DIR"

//go:embed ui_fallback/*
var fallbackUIFS embed.FS

// The real admin SPA is the panel entry of the one frontend project
// (ADR-015), embedded by the ui module: orbit commits the built dist so a
// consumer that mounts the module gets the full admin out of the box — no
// separate asset deployment (ADR-019: the admin ships as a normal Go
// dependency). The fleet's admin server embeds the same dist and serves
// its other entry.

// adminUIContentFS resolves the admin SPA filesystem, in order:
//  1. NUCLEUS_ADMIN_UI_DIR — a dev override pointing at a built dist on disk;
//  2. the SPA embedded in the binary (the shipped distribution);
//  3. the placeholder (only if the embedded dist is somehow absent).
func adminUIContentFS() fs.FS {
	if dir := strings.TrimSpace(os.Getenv(adminUIDirEnv)); dir != "" && adminUIBuildDirUsable(dir) {
		return os.DirFS(dir)
	}
	if sub := orbitui.Panel(); sub != nil && adminUIFSHasIndex(sub) {
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
	return injectHeadFragment(content, meta)
}

// injectHeadFragment inserts markup the panel wrote right after the
// document's opening <head>, the way injectHeadMeta inserts a meta tag: each
// insertion lands ahead of the previous ones. The fragment is inserted as
// is, so it must never carry a value that was not escaped or computed.
func injectHeadFragment(content []byte, fragment string) []byte {
	contentStr := string(content)
	if strings.Contains(contentStr, "<head>") {
		return []byte(strings.Replace(contentStr, "<head>", "<head>"+fragment, 1))
	}
	return []byte("<head>" + fragment + "</head>\n" + contentStr)
}

// injectHeadEnd inserts markup the panel wrote at the END of the document's
// <head>, after everything the build put there — the bundle's script and
// stylesheet among it. Without a </head> the fragment is appended. The
// fragment is inserted as is, under the same rule as injectHeadFragment.
func injectHeadEnd(content []byte, fragment string) []byte {
	contentStr := string(content)
	if i := strings.Index(contentStr, "</head>"); i >= 0 {
		return []byte(contentStr[:i] + fragment + contentStr[i:])
	}
	return []byte(contentStr + fragment)
}

func injectAdminPrefix(content []byte, prefix string) []byte {
	return injectHeadMeta(content, "nucleus-admin-prefix", NormalizePrefix(prefix))
}

// absoluteAssetPaths names the bundle's files from the panel's root. The
// build writes them relative to the document ("./assets/index-….js"), which
// resolves only while the SPA's path is one segment deep — and every screen
// was, until the dashboards (<prefix>/dashboards/<id>). A dashboard opened
// by its address, reloaded or bookmarked, asked for
// <prefix>/dashboards/assets/…, was answered the document instead of the
// script, and drew nothing. The chunks the bundle loads later resolve
// against the bundle's own URL, so only the document needs it.
func absoluteAssetPaths(content []byte, prefix string) []byte {
	root := html.EscapeString(strings.TrimSuffix(NormalizePrefix(prefix), "/"))
	out := strings.ReplaceAll(string(content), `="./assets/`, `="`+root+`/assets/`)
	out = strings.ReplaceAll(out, `="./favicon.svg"`, `="`+root+`/favicon.svg"`)
	return []byte(out)
}

// injectAdminTitle surfaces the configured panel title (Config.Title, default
// "Orbit") to the SPA as a meta tag — same mechanism as the prefix — so the
// login screen and the sidebar can render it. Served on every SPA page
// (handleSPA) and on the login page (renderLoginPage), so the title is
// available before any authenticated API call.
func injectAdminTitle(content []byte, title string) []byte {
	title = strings.TrimSpace(title)
	if title == "" {
		title = DefaultTitle
	}
	return injectHeadMeta(content, "nucleus-admin-title", title)
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
