package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AST-FSTRING guard: every adk.NewChatModelAgent must set our GenModelInput,
// or Eino's default formats the instruction as an FString and breaks on braces.
func TestChatModelAgentsUseSafeGenModelInput(t *testing.T) {
	fset := token.NewFileSet()
	filepath.Walk(".", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "NewChatModelAgent" && sel.Sel.Name != "NewChatModelAgentWithTemplate" {
				return true
			}
			found := false
			ast.Inspect(call, func(m ast.Node) bool {
				if kv, ok := m.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "GenModelInput" {
						if s, ok := kv.Value.(*ast.SelectorExpr); ok && s.Sel.Name == "GenModelInput" {
							found = true
						}
					}
				}
				return true
			})
			if !found {
				t.Errorf("%s: NewChatModelAgent without assistant.GenModelInput", fset.Position(call.Pos()))
			}
			return true
		})
		return nil
	})
}
