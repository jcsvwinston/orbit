// Package ui embeds the built frontend of Orbit: one project, two entries
// (ADR-015). dist/panel is the in-process panel, served by the root module
// under its prefix; dist/fleet is the fleet plane, served by the admin
// server at its UI root.
//
// The dist is committed: a consumer that requires the module gets the
// built frontend as a normal Go dependency, with no asset deployment of
// its own. CI rebuilds it and fails when the committed copy is stale.
//
// Every text file of at least 1 KiB carries two siblings the build wrote
// (tools/precompress.ts): <file>.gz and <file>.br, the same bytes encoded
// once, so a server can answer Accept-Encoding without compressing on each
// request.
//
// Each entry is its own embedded file system, and only the functions a
// program calls decide what it carries: the linker keeps an embedded
// variable only when something reachable reads it. The admin server calls
// Fleet and never Panel, so its binary carries the fleet entry and not the
// panel's (TestEmbeddedDist_EachEntryLinksAlone).
package ui

import (
	"embed"
	"errors"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

//go:embed all:dist/panel
var panelFS embed.FS

//go:embed all:dist/fleet
var fleetFS embed.FS

// Panel is the in-process panel's entry, or nil when the dist lacks it.
func Panel() fs.FS { return entry(panelFS, "dist/panel") }

// Fleet is the fleet plane's entry, or nil when the dist lacks it.
func Fleet() fs.FS { return entry(fleetFS, "dist/fleet") }

// Dist is the whole embedded dist: panel/ and fleet/. A program that calls
// it carries both entries.
func Dist() fs.FS {
	return distFS{"panel": entry(panelFS, "dist/panel"), "fleet": entry(fleetFS, "dist/fleet")}
}

func entry(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		return nil
	}
	if info, err := fs.Stat(sub, "index.html"); err != nil || info.IsDir() {
		return nil
	}
	return sub
}

// distFS joins the two entries under their directory names, the tree
// dist/ was when one variable embedded it.
type distFS map[string]fs.FS

func (d distFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return &rootDir{entries: d.rootEntries()}, nil
	}
	top, rest, _ := strings.Cut(name, "/")
	sub := d[top]
	if sub == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	if rest == "" {
		rest = "."
	}
	f, err := sub.Open(rest)
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			pe.Path = name
		}
		return nil, err
	}
	return f, nil
}

func (d distFS) rootEntries() []fs.DirEntry {
	var out []fs.DirEntry
	for name, sub := range d {
		if sub == nil {
			continue
		}
		out = append(out, dirEntry(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// rootDir is the directory listing of dist/ itself.
type rootDir struct {
	entries []fs.DirEntry
	read    int
}

func (r *rootDir) Stat() (fs.FileInfo, error) { return dirEntry("."), nil }
func (r *rootDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: errors.New("is a directory")}
}
func (r *rootDir) Close() error { return nil }
func (r *rootDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := r.entries[r.read:]
	if n <= 0 {
		r.read = len(r.entries)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	if n > len(rest) {
		n = len(rest)
	}
	r.read += n
	return rest[:n], nil
}

// dirEntry describes one of the two entry directories (or the root).
type dirEntry string

func (d dirEntry) Name() string               { return path.Base(string(d)) }
func (d dirEntry) IsDir() bool                { return true }
func (d dirEntry) Type() fs.FileMode          { return fs.ModeDir }
func (d dirEntry) Info() (fs.FileInfo, error) { return d, nil }
func (d dirEntry) Size() int64                { return 0 }
func (d dirEntry) Mode() fs.FileMode          { return fs.ModeDir | 0o555 }
func (d dirEntry) ModTime() time.Time         { return time.Time{} }
func (d dirEntry) Sys() any                   { return nil }
