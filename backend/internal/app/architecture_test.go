package app_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Protect the agreed handler -> service -> repository boundary from accidental
// regression. Module repositories still enforce authorization within a DB tx.
func TestArchitectureHTTPHandlersUseServices(t *testing.T) {
	for _, module := range []string{"auth", "users", "teachers", "students", "parents", "programs", "curriculum", "groups", "lessons", "attendance", "tasks", "dashboard", "audit"} {
		t.Run(module, func(t *testing.T) {
			root := filepath.Join("..", "modules", module)
			files, err := filepath.Glob(filepath.Join(root, "*handler*.go"))
			if err != nil || len(files) == 0 {
				t.Fatalf("missing HTTP layer: %v", err)
			}
			if _, err = os.Stat(filepath.Join(root, "service.go")); err != nil {
				t.Fatalf("missing service: %v", err)
			}
			if _, err = os.Stat(filepath.Join(root, "repository.go")); err != nil {
				t.Fatalf("missing repository: %v", err)
			}
			for _, file := range files {
				if strings.HasSuffix(file, "_test.go") {
					continue
				}
				parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
				if err != nil {
					t.Fatal(err)
				}
				for _, spec := range parsed.Imports {
					name, _ := strconv.Unquote(spec.Path.Value)
					if strings.Contains(name, "/pgx/") || name == "database/sql" || strings.Contains(name, "/platform/database") {
						t.Errorf("HTTP handler imports persistence: %s", name)
					}
				}
				ast.Inspect(parsed, func(node ast.Node) bool {
					if spec, ok := node.(*ast.TypeSpec); ok && spec.Name.Name == "Handler" {
						fields, ok := spec.Type.(*ast.StructType)
						if !ok {
							return true
						}
						service := false
						for _, field := range fields.Fields.List {
							for _, name := range field.Names {
								if name.Name == "Repo" || name.Name == "Pool" {
									t.Errorf("HTTP handler depends directly on %s", name.Name)
								}
								if name.Name == "Service" {
									service = true
								}
							}
						}
						if !service {
							t.Error("HTTP handler must depend on a service")
						}
					}
					return true
				})
			}
		})
	}
	if _, err := os.Stat(filepath.Join("..", "modules", "families")); !os.IsNotExist(err) {
		t.Fatal("combined families implementation must not remain an active module")
	}
}
