package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreDoesNotExposeTransactionHooks(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举 core 源文件失败: %v", err)
	}

	forbiddenTypes := map[string]struct{}{
		"TxHookFunc":    {},
		"TxHookManager": {},
	}
	forbiddenMethods := map[string]struct{}{
		"RegisterCommit":   {},
		"RegisterRollback": {},
	}

	fileSet := token.NewFileSet()
	for _, path := range files {
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("解析 %s 失败: %v", path, parseErr)
		}

		for _, declaration := range file.Decls {
			switch typedDeclaration := declaration.(type) {
			case *ast.GenDecl:
				for _, spec := range typedDeclaration.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if _, forbidden := forbiddenTypes[typeSpec.Name.Name]; forbidden {
						t.Errorf("Core 不应声明事务类型 %s: %s", typeSpec.Name.Name, path)
					}
					if typeSpec.Name.Name != "Service" {
						continue
					}
					serviceType, ok := typeSpec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range serviceType.Fields.List {
						for _, name := range field.Names {
							if name.Name == "txHookMgr" {
								t.Errorf("Core Service 不应声明事务字段 %s: %s", name.Name, path)
							}
						}
					}
				}
			case *ast.FuncDecl:
				if typedDeclaration.Recv == nil {
					continue
				}
				if _, forbidden := forbiddenMethods[typedDeclaration.Name.Name]; forbidden {
					t.Errorf("Core 不应声明事务方法 %s: %s", typedDeclaration.Name.Name, path)
				}
			}
		}
	}
}
