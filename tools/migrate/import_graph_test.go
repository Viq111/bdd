package migrate_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationImportsAreIsolated(t *testing.T) {
	root := filepath.Join("..", "..")
	check := func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.ToSlash(rel), "tools/migrate/") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			if strings.Contains(strings.Trim(imp.Path.Value, "\""), "/tools/migrate/") {
				return &importLeak{path: rel}
			}
		}
		return nil
	}
	if err := filepath.Walk(root, check); err != nil {
		t.Fatal(err)
	}
	// Exercise the negative branch independently of the repository tree.
	file, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", `package synthetic; import _ "github.com/viq111/bdd/tools/migrate/internal/sourcebd"`, parser.ImportsOnly)
	if err != nil || !hasMigrationImport(file) {
		t.Fatalf("negative import was not detected: %v", err)
	}
}

type importLeak struct{ path string }

func (e *importLeak) Error() string { return "migration import outside tools/migrate: " + e.path }
func hasMigrationImport(file *ast.File) bool {
	for _, imp := range file.Imports {
		if strings.Contains(imp.Path.Value, "/tools/migrate/") {
			return true
		}
	}
	return false
}
