package bdd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoNotImplementedSentinel fails if a generic not-implemented sentinel
// identifier is reintroduced anywhere in production source (the root
// package or internal/cli). errNotImplemented itself was removed by bd
// bdd-owt4; this is the durable form of the finding-8.4 audit that keeps it
// gone, whatever name a future stub uses.
func TestNoNotImplementedSentinel(t *testing.T) {
	const forbidden = "errnotimplemented"

	for _, dir := range surfaceGuardDirs(t) {
		for _, path := range goSourceFiles(t, dir) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if strings.Contains(strings.ToLower(string(src)), forbidden) {
				t.Errorf("%s: contains a reintroduced not-implemented sentinel (%s); commands and exported methods must be fully implemented or removed, not stubbed", path, forbidden)
			}
		}
	}
}

// TestNoAlwaysFailingSurface fails if any CLI command function (internal/cli)
// or exported method/function of the root package unconditionally returns a
// hardcoded error - the general shape of an unimplemented stub, regardless
// of what its sentinel is named. A function that forwards the outcome of
// another call (e.g. `return db.ready()`) is fine: only a body whose single
// statement manufactures its own error, ignoring its inputs, counts as
// always-failing.
func TestNoAlwaysFailingSurface(t *testing.T) {
	for _, dir := range surfaceGuardDirs(t) {
		exportedOnly := dir == "."
		for _, path := range goSourceFiles(t, dir) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if exportedOnly && !fn.Name.IsExported() {
					continue
				}
				if desc, bad := alwaysFailingBody(fn); bad {
					pos := fset.Position(fn.Pos())
					t.Errorf("%s:%d: %s always returns a hardcoded error (%s); implement it or remove it instead of leaving a stub", path, pos.Line, funcLabel(fn), desc)
				}
			}
		}
	}
}

// surfaceGuardDirs is the public-surface scope this card covers: the root
// package (package bdd, its exported API) and internal/cli (the command
// implementations invoked by cmd/bdd).
func surfaceGuardDirs(t *testing.T) []string {
	t.Helper()
	for _, dir := range []string{".", "internal/cli"} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("surface guard scope %q: %v (test must run from the module root)", dir, err)
		}
	}
	return []string{".", "internal/cli"}
}

// goSourceFiles lists the non-test, non-generated .go files directly inside
// dir (no recursion: each guarded directory is a single package).
func goSourceFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	return files
}

func funcLabel(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "func " + fn.Name.Name
	}
	return "method " + fn.Name.Name
}

// alwaysFailingBody reports whether fn's entire body is a single return
// statement that manufactures a non-nil error (errors.New/fmt.Errorf, a
// package-level sentinel identifier or selector, or a composite literal),
// as opposed to forwarding the result of another call or a parameter.
func alwaysFailingBody(fn *ast.FuncDecl) (string, bool) {
	// Unwrap/Is/As are the standard error-interface methods: returning a
	// fixed sentinel from Unwrap (e.g. "return ErrInvalidArgument") is the
	// idiomatic implementation, not a not-implemented stub.
	switch fn.Name.Name {
	case "Unwrap", "Is", "As":
		return "", false
	}
	if !lastResultIsError(fn) {
		return "", false
	}

	body := fn.Body
	if len(body.List) != 1 {
		return "", false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return "", false
	}

	params := paramNames(fn)
	last := ret.Results[len(ret.Results)-1]
	return hardcodedErrorExpr(last, params)
}

// lastResultIsError reports whether fn's final return value is the builtin
// error type - the guard only applies to functions that can fail, not to
// constructors or accessors that happen to have a one-statement body.
func lastResultIsError(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
		return false
	}
	last := fn.Type.Results.List[len(fn.Type.Results.List)-1]
	id, ok := last.Type.(*ast.Ident)
	return ok && id.Name == "error"
}

func paramNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	if fn.Recv != nil {
		for _, f := range fn.Recv.List {
			for _, n := range f.Names {
				names[n.Name] = true
			}
		}
	}
	if fn.Type.Params != nil {
		for _, f := range fn.Type.Params.List {
			for _, n := range f.Names {
				names[n.Name] = true
			}
		}
	}
	return names
}

// hardcodedErrorExpr classifies the final return value of a single-statement
// function body. It flags expressions that are guaranteed non-nil
// regardless of the function's inputs; it does not flag a bare parameter
// (a passthrough) or a call to another function/method (which may return
// nil on success).
func hardcodedErrorExpr(e ast.Expr, params map[string]bool) (string, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		if v.Name == "nil" || params[v.Name] {
			return "", false
		}
		return "bare identifier " + v.Name, true
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok && params[id.Name] {
			return "", false
		}
		return "package-level reference " + selectorString(v), true
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok {
				switch {
				case pkg.Name == "errors" && sel.Sel.Name == "New":
					return "errors.New(...)", true
				case pkg.Name == "fmt" && sel.Sel.Name == "Errorf":
					return "fmt.Errorf(...)", true
				}
			}
		}
		return "", false
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			if _, ok := v.X.(*ast.CompositeLit); ok {
				return "composite literal", true
			}
		}
		return "", false
	default:
		return "", false
	}
}

func selectorString(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return id.Name + "." + sel.Sel.Name
	}
	return sel.Sel.Name
}
