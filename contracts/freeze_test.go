// Package contracts pins orbit's frozen public API surface.
//
// The v1.0 promise (docs/V1_GATE.md §A-1/§A-3, ADR-001 D-freeze) covers two
// packages: the module wiring surface (`orbit` root: Config, Module) and the
// `datasource` contract that third-party data sources implement. This test is
// the machinery behind that promise: a PR that removes or renames a frozen
// symbol fails here instead of relying on review memory.
//
// The mechanism is a scaled-down port of nucleus's contracts/freeze_test.go
// (same baseline-line format `<importPath> <kind>:<Symbol>`), including its
// two hard-won correctness fixes: go/doc files constructor functions and
// typed constants under the TYPE's Funcs/Consts, not the package-level lists,
// so both must be walked explicitly or NewXxx constructors and enum-style
// consts are invisible to the freeze.
//
// Deliberate, reviewed surface changes regenerate the baseline with:
//
//	ORBIT_UPDATE_CONTRACT_BASELINE=1 go test ./contracts
package contracts

import (
	"bufio"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

type frozenPackage struct {
	importPath string
	dir        string // relative to the repo root
}

// frozenPackages is the v1.0 freeze scope. Additions here are contract
// EXPANSIONS and require a gate/ADR note; removals are breaking.
func frozenPackages() []frozenPackage {
	return []frozenPackage{
		{importPath: "github.com/jcsvwinston/orbit", dir: "."},
		{importPath: "github.com/jcsvwinston/orbit/datasource", dir: "datasource"},
	}
}

const baselineFile = "api_exported_symbols.txt"

func TestStableAPISurfaceIsFrozen(t *testing.T) {
	current := stableAPISymbolBaselineLines(t)

	if os.Getenv("ORBIT_UPDATE_CONTRACT_BASELINE") == "1" {
		writeBaselineLines(t, current, "baseline", baselineFile)
		t.Logf("baseline regenerated with %d symbols", len(current))
		return
	}

	baseline := readBaselineLines(t, "baseline", baselineFile)

	currentSet := toSet(current)
	baselineSet := toSet(baseline)

	var removed, added []string
	for _, line := range baseline {
		if _, ok := currentSet[line]; !ok {
			removed = append(removed, line)
		}
	}
	for _, line := range current {
		if _, ok := baselineSet[line]; !ok {
			added = append(added, line)
		}
	}

	if len(removed) > 0 {
		t.Errorf("frozen symbols REMOVED from the public surface (breaking; needs a major or a revert):\n  %s",
			strings.Join(removed, "\n  "))
	}
	if len(added) > 0 {
		t.Errorf("new exported symbols not in the frozen baseline (deliberate additions rebaseline with ORBIT_UPDATE_CONTRACT_BASELINE=1):\n  %s",
			strings.Join(added, "\n  "))
	}
}

func stableAPISymbolBaselineLines(t *testing.T) []string {
	t.Helper()
	repoRoot := filepath.Dir(contractsDir(t))

	lines := make([]string, 0, 128)
	for _, pkg := range frozenPackages() {
		dir := filepath.Join(repoRoot, filepath.FromSlash(pkg.dir))
		for _, symbol := range exportedSymbolsForPackage(t, dir) {
			lines = append(lines, pkg.importPath+" "+symbol)
		}
	}
	sort.Strings(lines)
	return dedupeSorted(lines)
}

func exportedSymbolsForPackage(t *testing.T, dir string) []string {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(info os.FileInfo) bool {
		name := info.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}

	var target *ast.Package
	for name, pkg := range pkgs {
		if name == "main" {
			continue
		}
		target = pkg
		break
	}
	if target == nil {
		t.Fatalf("unable to resolve package in %s", dir)
	}

	docPkg := doc.New(target, "", doc.AllDecls)
	symbols := make([]string, 0, 64)
	for _, v := range docPkg.Vars {
		for _, name := range v.Names {
			if ast.IsExported(name) {
				symbols = append(symbols, "var:"+name)
			}
		}
	}
	for _, c := range docPkg.Consts {
		for _, name := range c.Names {
			if ast.IsExported(name) {
				symbols = append(symbols, "const:"+name)
			}
		}
	}
	for _, fn := range docPkg.Funcs {
		if ast.IsExported(fn.Name) {
			symbols = append(symbols, "func:"+fn.Name)
		}
	}
	for _, typ := range docPkg.Types {
		if !ast.IsExported(typ.Name) {
			continue
		}
		symbols = append(symbols, "type:"+typ.Name)
		symbols = append(symbols, exportedMembersFromTypeDecl(typ.Decl, typ.Name)...)
		// go/doc files type-associated consts and constructor functions under
		// the TYPE, not the package-level lists above (nucleus's
		// constructor-gap lesson, 2026-05-20) — walk both or they are
		// invisible to the freeze.
		for _, c := range typ.Consts {
			for _, name := range c.Names {
				if ast.IsExported(name) {
					symbols = append(symbols, "const:"+name)
				}
			}
		}
		for _, fn := range typ.Funcs {
			if ast.IsExported(fn.Name) {
				symbols = append(symbols, "func:"+fn.Name)
			}
		}
		for _, method := range typ.Methods {
			if ast.IsExported(method.Name) {
				symbols = append(symbols, "method:"+typ.Name+"."+method.Name)
			}
		}
	}
	sort.Strings(symbols)
	return dedupeSorted(symbols)
}

func exportedMembersFromTypeDecl(decl *ast.GenDecl, owner string) []string {
	if decl == nil {
		return nil
	}
	out := make([]string, 0, 16)
	for _, spec := range decl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != owner {
			continue
		}
		switch node := typeSpec.Type.(type) {
		case *ast.StructType:
			for _, field := range node.Fields.List {
				for _, name := range field.Names {
					if ast.IsExported(name.Name) {
						out = append(out, "field:"+owner+"."+name.Name)
					}
				}
			}
		case *ast.InterfaceType:
			for _, field := range node.Methods.List {
				for _, name := range field.Names {
					if ast.IsExported(name.Name) {
						out = append(out, "iface-method:"+owner+"."+name.Name)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return dedupeSorted(out)
}

func readBaselineLines(t *testing.T, rel ...string) []string {
	t.Helper()
	base := filepath.Join(contractsDir(t), filepath.Join(rel...))
	f, err := os.Open(base)
	if err != nil {
		t.Fatalf("open baseline %s (regenerate with ORBIT_UPDATE_CONTRACT_BASELINE=1): %v", base, err)
	}
	defer f.Close()

	out := make([]string, 0, 64)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read baseline %s: %v", base, err)
	}
	sort.Strings(out)
	return dedupeSorted(out)
}

func writeBaselineLines(t *testing.T, lines []string, rel ...string) {
	t.Helper()
	base := filepath.Join(contractsDir(t), filepath.Join(rel...))
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatalf("create baseline dir for %s: %v", base, err)
	}
	data := strings.Join(lines, "\n")
	if data != "" {
		data += "\n"
	}
	if err := os.WriteFile(base, []byte(data), 0o644); err != nil {
		t.Fatalf("write baseline %s: %v", base, err)
	}
}

func contractsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve current file path")
	}
	return filepath.Dir(file)
}

func dedupeSorted(items []string) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items))
	prev := ""
	for i, item := range items {
		if i == 0 || item != prev {
			out = append(out, item)
		}
		prev = item
	}
	return out
}

func toSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}
