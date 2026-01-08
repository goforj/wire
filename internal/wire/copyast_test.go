// Copyright 2026 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package wire

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestCopyASTPreservesIdents(t *testing.T) {
	src := `package p
func f(ch chan int, s []int, m map[string]int) {
	var x int
	x++
	x = x + 1
	if x > 0 {
		x = -x
	} else {
		x = +x
	}
	for i := 0; i < 3; i++ {
		_ = i
	}
	for _, v := range s {
		_ = v
	}
	switch x {
	case 1:
		x = 2
	default:
	}
	select {
	case ch <- x:
	default:
	}
	defer g()
	go g()
	_ = m["k"]
	_ = []int{1, 2}[0]
	_ = func(a int) int { return a }(1)
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "orig.go", src, 0)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(file.Decls) == 0 {
		t.Fatal("expected declarations")
	}
	fn, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok {
		t.Fatal("expected func decl")
	}
	copied := copyAST(fn).(*ast.FuncDecl)

	origIdents := make(map[token.Pos]*ast.Ident)
	ast.Inspect(fn, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok {
			origIdents[ident.Pos()] = ident
		}
		return true
	})
	ast.Inspect(copied, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok {
			if origIdent, ok := origIdents[ident.Pos()]; ok {
				if origIdent != ident {
					t.Fatalf("ident at pos %v not preserved", ident.Pos())
				}
			}
		}
		return true
	})
}

func TestCopyASTHelpers(t *testing.T) {
	m := make(map[ast.Node]ast.Node)
	if identFromMap(m, nil) != nil {
		t.Fatal("expected nil ident")
	}
	if blockStmtFromMap(m, nil) != nil {
		t.Fatal("expected nil block stmt")
	}
	if callExprFromMap(m, nil) != nil {
		t.Fatal("expected nil call expr")
	}
	if basicLitFromMap(m, nil) != nil {
		t.Fatal("expected nil basic lit")
	}
	if funcTypeFromMap(m, nil) != nil {
		t.Fatal("expected nil func type")
	}

	ident := &ast.Ident{Name: "x"}
	block := &ast.BlockStmt{}
	call := &ast.CallExpr{}
	lit := &ast.BasicLit{}
	fn := &ast.FuncType{}
	m[ident] = ident
	m[block] = block
	m[call] = call
	m[lit] = lit
	m[fn] = fn

	if got := identFromMap(m, ident); got != ident {
		t.Fatal("identFromMap returned unexpected value")
	}
	if got := blockStmtFromMap(m, block); got != block {
		t.Fatal("blockStmtFromMap returned unexpected value")
	}
	if got := callExprFromMap(m, call); got != call {
		t.Fatal("callExprFromMap returned unexpected value")
	}
	if got := basicLitFromMap(m, lit); got != lit {
		t.Fatal("basicLitFromMap returned unexpected value")
	}
	if got := funcTypeFromMap(m, fn); got != fn {
		t.Fatal("funcTypeFromMap returned unexpected value")
	}
}

func TestCopyASTTypeDecl(t *testing.T) {
	src := `package p
type (
	S struct{ A int }
	I interface{ M() }
	M map[string]int
	C chan<- int
	F func(int) string
	P *S
)
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "types.go", src, 0)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(file.Decls) == 0 {
		t.Fatal("expected declarations")
	}
	gen, ok := file.Decls[0].(*ast.GenDecl)
	if !ok {
		t.Fatal("expected gen decl")
	}
	if copy := copyAST(gen); copy == nil {
		t.Fatal("expected copy")
	}
}

func TestCopyASTCoversNodes(t *testing.T) {
	t.Parallel()

	const src = `package p

import "fmt"

type S struct {
	A int
	B string ` + "`json:\"b\"`" + `
}

type I interface{ M() }

func f(ch chan int, m map[string]int, s []int, a [2]int) {
L:
	for i := 0; i < 1; i++ {
		if i == 0 {
			goto L
		}
	}
	switch x := interface{}(s); v := x.(type) {
	case []int:
		_ = v
	default:
	}
	select {
	case ch <- 1:
	default:
	}
	for _, v := range s {
		_ = v
	}
	go f(nil, nil, nil, [2]int{})
	defer fmt.Println("x")
	_ = m["k"]
	_ = s[0:1]
	_ = a[:]
	_ = S{A: 1, B: "x"}
	_ = func(x int) int { return x }(1)
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	origFn := findFirstFuncDecl(t, file)
	copiedFn := copyAST(origFn).(*ast.FuncDecl)
	if origFn.Name != copiedFn.Name {
		t.Fatal("expected identifier identity to be preserved")
	}

	nodes := []ast.Node{
		&ast.BadDecl{From: 1, To: 2},
		&ast.BadExpr{From: 3, To: 4},
		&ast.BadStmt{From: 5, To: 6},
		&ast.GenDecl{
			Tok: token.IMPORT,
			Specs: []ast.Spec{
				&ast.ImportSpec{
					Name: ast.NewIdent("fmt"),
					Path: &ast.BasicLit{Kind: token.STRING, Value: "\"fmt\""},
				},
			},
		},
		&ast.GenDecl{
			Tok: token.TYPE,
			Specs: []ast.Spec{
				&ast.TypeSpec{
					Name: ast.NewIdent("T"),
					Type: &ast.StructType{Fields: &ast.FieldList{}},
				},
			},
		},
		&ast.GenDecl{
			Tok: token.TYPE,
			Specs: []ast.Spec{
				&ast.TypeSpec{
					Name: ast.NewIdent("I"),
					Type: &ast.InterfaceType{Methods: &ast.FieldList{}},
				},
			},
		},
		&ast.SwitchStmt{
			Body: &ast.BlockStmt{},
		},
		&ast.GenDecl{
			Tok: token.VAR,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names:  []*ast.Ident{ast.NewIdent("x")},
					Type:   ast.NewIdent("int"),
					Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "1"}},
				},
			},
		},
		&ast.DeclStmt{
			Decl: &ast.GenDecl{
				Tok: token.VAR,
				Specs: []ast.Spec{
					&ast.ValueSpec{
						Names: []*ast.Ident{ast.NewIdent("y")},
						Type:  ast.NewIdent("int"),
					},
				},
			},
		},
		&ast.ParenExpr{X: &ast.BasicLit{Kind: token.INT, Value: "1"}},
		&ast.UnaryExpr{Op: token.SUB, X: &ast.BasicLit{Kind: token.INT, Value: "2"}},
		&ast.StarExpr{X: ast.NewIdent("ptr")},
	}
	for _, node := range nodes {
		if copyAST(node) == nil {
			t.Fatalf("expected copy for %T", node)
		}
	}
}

func findFirstFuncDecl(t *testing.T, file *ast.File) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatal("expected function declaration")
	return nil
}
