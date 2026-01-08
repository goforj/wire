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
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

func TestFindInjectorBuildVariants(t *testing.T) {
	t.Parallel()

	info := &types.Info{
		Uses: make(map[*ast.Ident]types.Object),
	}
	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wireIdent := ast.NewIdent("wire")
	buildIdent := ast.NewIdent("Build")
	info.Uses[wireIdent] = types.NewPkgName(token.NoPos, nil, "wire", wirePkg)
	info.Uses[buildIdent] = types.NewFunc(token.NoPos, wirePkg, "Build", nil)

	buildCall := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   wireIdent,
			Sel: buildIdent,
		},
	}

	fn := &ast.FuncDecl{
		Name: ast.NewIdent("Init"),
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: buildCall}}},
	}
	if call, err := findInjectorBuild(info, fn); err != nil || call == nil {
		t.Fatalf("expected build call, got call=%v err=%v", call, err)
	}

	panicIdent := ast.NewIdent("panic")
	info.Uses[panicIdent] = types.Universe.Lookup("panic")
	panicCall := &ast.CallExpr{
		Fun:  panicIdent,
		Args: []ast.Expr{buildCall},
	}
	fn = &ast.FuncDecl{
		Name: ast.NewIdent("Init"),
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: panicCall}}},
	}
	if call, err := findInjectorBuild(info, fn); err != nil || call == nil {
		t.Fatalf("expected panic-wrapped build call, got call=%v err=%v", call, err)
	}

	otherCall := &ast.CallExpr{Fun: ast.NewIdent("Other")}
	fn = &ast.FuncDecl{
		Name: ast.NewIdent("Init"),
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.ExprStmt{X: buildCall},
			&ast.ExprStmt{X: otherCall},
		}},
	}
	if call, err := findInjectorBuild(info, fn); err == nil {
		t.Fatalf("expected invalid injector error, got call=%v err=%v", call, err)
	}

	fn = &ast.FuncDecl{
		Name: ast.NewIdent("Init"),
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{}}},
	}
	if call, err := findInjectorBuild(info, fn); err != nil || call != nil {
		t.Fatalf("expected no build call, got call=%v err=%v", call, err)
	}

	fn = &ast.FuncDecl{
		Name: ast.NewIdent("Init"),
		Type: &ast.FuncType{},
		Body: nil,
	}
	if call, err := findInjectorBuild(info, fn); err != nil || call != nil {
		t.Fatalf("expected no build call for nil body, got call=%v err=%v", call, err)
	}
}

func TestCheckFieldErrors(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("example.com/p", "p")
	fields := []*types.Var{
		types.NewVar(token.NoPos, pkg, "Foo", types.Typ[types.Int]),
		types.NewVar(token.NoPos, pkg, "Bar", types.Typ[types.String]),
	}
	tags := []string{`wire:"-"`, ""}
	st := types.NewStruct(fields, tags)

	if _, err := checkField(ast.NewIdent("Foo"), st); err == nil {
		t.Fatal("expected non-string field error")
	}
	if _, err := checkField(&ast.BasicLit{Kind: token.STRING, Value: "\"Foo\""}, st); err == nil {
		t.Fatal("expected prevented field error")
	}
	if _, err := checkField(&ast.BasicLit{Kind: token.STRING, Value: "\"Missing\""}, st); err == nil {
		t.Fatal("expected missing field error")
	}
}

func TestProcessStructProviderCases(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Uses:  make(map[*ast.Ident]types.Object),
	}

	pkg := types.NewPackage("example.com/p", "p")
	typeName := types.NewTypeName(token.NoPos, pkg, "Foo", nil)
	fields := []*types.Var{
		types.NewVar(token.NoPos, pkg, "Skip", types.Typ[types.Int]),
		types.NewVar(token.NoPos, pkg, "Keep", types.Typ[types.String]),
	}
	tags := []string{`wire:"-"`, ""}
	st := types.NewStruct(fields, tags)
	named := types.NewNamed(typeName, st, nil)
	ptr := types.NewPointer(named)

	typeIdent := ast.NewIdent("Foo")
	info.Uses[typeIdent] = typeName
	newCall := &ast.CallExpr{Fun: ast.NewIdent("new"), Args: []ast.Expr{typeIdent}}
	info.Types[newCall] = types.TypeAndValue{Type: ptr}

	allCall := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: ast.NewIdent("wire"), Sel: ast.NewIdent("Struct")},
		Args: []ast.Expr{newCall, &ast.BasicLit{Kind: token.STRING, Value: "\"*\""}},
	}
	provider, err := processStructProvider(fset, info, allCall)
	if err != nil {
		t.Fatalf("expected struct provider, got err=%v", err)
	}
	if len(provider.Args) != 1 || provider.Args[0].FieldName != "Keep" {
		t.Fatalf("expected prevented field to be skipped, got %+v", provider.Args)
	}

	missingFieldCall := &ast.CallExpr{
		Fun:  allCall.Fun,
		Args: []ast.Expr{newCall, &ast.BasicLit{Kind: token.STRING, Value: "\"Missing\""}},
	}
	if _, err := processStructProvider(fset, info, missingFieldCall); err == nil {
		t.Fatal("expected missing field error")
	}

	noArgsCall := &ast.CallExpr{Fun: allCall.Fun}
	if _, err := processStructProvider(fset, info, noArgsCall); err == nil {
		t.Fatal("expected no-arg struct error")
	}

	nonPtrIdent := ast.NewIdent("NonPtr")
	info.Types[nonPtrIdent] = types.TypeAndValue{Type: types.Typ[types.Int]}
	nonPtrCall := &ast.CallExpr{Fun: allCall.Fun, Args: []ast.Expr{nonPtrIdent}}
	if _, err := processStructProvider(fset, info, nonPtrCall); err == nil {
		t.Fatal("expected non-pointer struct error")
	}

	nonStruct := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Number", nil), types.Typ[types.Int], nil)
	nonStructIdent := ast.NewIdent("Number")
	info.Types[nonStructIdent] = types.TypeAndValue{Type: types.NewPointer(nonStruct)}
	nonStructCall := &ast.CallExpr{Fun: allCall.Fun, Args: []ast.Expr{nonStructIdent}}
	if _, err := processStructProvider(fset, info, nonStructCall); err == nil {
		t.Fatal("expected non-struct pointer error")
	}
}

func TestProcessStructProviderDuplicateFields(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Uses:  make(map[*ast.Ident]types.Object),
	}

	pkg := types.NewPackage("example.com/p", "p")
	typeName := types.NewTypeName(token.NoPos, pkg, "Dup", nil)
	fields := []*types.Var{
		types.NewVar(token.NoPos, pkg, "First", types.Typ[types.Int]),
		types.NewVar(token.NoPos, pkg, "Second", types.Typ[types.Int]),
	}
	st := types.NewStruct(fields, []string{"", ""})
	named := types.NewNamed(typeName, st, nil)
	ptr := types.NewPointer(named)

	typeIdent := ast.NewIdent("Dup")
	info.Uses[typeIdent] = typeName
	newCall := &ast.CallExpr{Fun: ast.NewIdent("new"), Args: []ast.Expr{typeIdent}}
	info.Types[newCall] = types.TypeAndValue{Type: ptr}

	call := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: ast.NewIdent("wire"), Sel: ast.NewIdent("Struct")},
		Args: []ast.Expr{newCall, &ast.BasicLit{Kind: token.STRING, Value: "\"*\""}},
	}
	if _, err := processStructProvider(fset, info, call); err == nil {
		t.Fatal("expected duplicate field error")
	}
}

func TestProcessFuncProviderErrors(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("example.com/p", "p")
	fset := token.NewFileSet()

	params := types.NewTuple(
		types.NewVar(token.NoPos, pkg, "a", types.Typ[types.Int]),
		types.NewVar(token.NoPos, pkg, "b", types.Typ[types.Int]),
	)
	results := types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Typ[types.String]))
	sig := types.NewSignatureType(nil, nil, nil, params, results, false)
	fn := types.NewFunc(token.NoPos, pkg, "Provide", sig)
	if _, errs := processFuncProvider(fset, fn); len(errs) == 0 {
		t.Fatal("expected duplicate param error")
	}

	noResultsSig := types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(), false)
	fn = types.NewFunc(token.NoPos, pkg, "ProvideNone", noResultsSig)
	if _, errs := processFuncProvider(fset, fn); len(errs) == 0 {
		t.Fatal("expected no-results error")
	}
}

func TestFuncOutputSignatures(t *testing.T) {
	t.Parallel()

	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(), false)
	if _, err := funcOutput(sig); err == nil {
		t.Fatal("expected no return values error")
	}

	results := types.NewTuple(
		types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]),
	)
	sig = types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	if _, err := funcOutput(sig); err == nil {
		t.Fatal("expected invalid second return error")
	}

	results = types.NewTuple(
		types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, nil, "", cleanupType),
		types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]),
	)
	sig = types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	if _, err := funcOutput(sig); err == nil {
		t.Fatal("expected invalid third return error")
	}

	results = types.NewTuple(
		types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, nil, "", errorType),
		types.NewVar(token.NoPos, nil, "", errorType),
	)
	sig = types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	if _, err := funcOutput(sig); err == nil {
		t.Fatal("expected invalid second return error")
	}

	results = types.NewTuple(
		types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, nil, "", cleanupType),
	)
	sig = types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	if got, err := funcOutput(sig); err != nil || !got.cleanup {
		t.Fatalf("expected cleanup signature, got=%+v err=%v", got, err)
	}

	results = types.NewTuple(
		types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, nil, "", errorType),
	)
	sig = types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	if got, err := funcOutput(sig); err != nil || !got.err {
		t.Fatalf("expected error signature, got=%+v err=%v", got, err)
	}

	results = types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]))
	sig = types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	if got, err := funcOutput(sig); err != nil || got.out == nil {
		t.Fatalf("expected single return signature, got=%+v err=%v", got, err)
	}

	results = types.NewTuple(
		types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, nil, "", cleanupType),
		types.NewVar(token.NoPos, nil, "", errorType),
	)
	sig = types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	if got, err := funcOutput(sig); err != nil || !got.cleanup || !got.err {
		t.Fatalf("expected cleanup+error signature, got=%+v err=%v", got, err)
	}
}

func TestAllFields(t *testing.T) {
	t.Parallel()

	if allFields(&ast.CallExpr{}) {
		t.Fatal("expected false for empty call")
	}
	if allFields(&ast.CallExpr{Args: []ast.Expr{ast.NewIdent("x")}}) {
		t.Fatal("expected false for one arg")
	}
	if allFields(&ast.CallExpr{Args: []ast.Expr{ast.NewIdent("x"), ast.NewIdent("y")}}) {
		t.Fatal("expected false for non-literal")
	}
	if !allFields(&ast.CallExpr{Args: []ast.Expr{ast.NewIdent("x"), &ast.BasicLit{Kind: token.STRING, Value: "\"*\""}}}) {
		t.Fatal("expected true for wildcard literal")
	}
}

func TestObjectCacheEnsurePackage(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	pkg := &packages.Package{PkgPath: "example.com/p", Fset: fset}
	oc := newObjectCache([]*packages.Package{pkg}, nil)

	if got, errs := oc.ensurePackage(pkg.PkgPath); len(errs) != 0 || got != pkg {
		t.Fatalf("expected existing package without errors, got pkg=%v errs=%v", got, errs)
	}
	if _, errs := oc.ensurePackage("missing.example.com"); len(errs) == 0 {
		t.Fatal("expected missing package error")
	}
}

func TestProcessExprErrors(t *testing.T) {
	t.Parallel()

	oc := &objectCache{
		fset:     token.NewFileSet(),
		packages: make(map[string]*packages.Package),
		objects:  make(map[objRef]objCacheEntry),
		hasher:   typeutil.MakeHasher(),
	}
	info := &types.Info{
		Uses:  make(map[*ast.Ident]types.Object),
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	call := &ast.CallExpr{Fun: ast.NewIdent("Unknown")}
	if _, errs := oc.processExpr(info, "example.com/p", call, ""); len(errs) == 0 {
		t.Fatal("expected unknown function error")
	}

	nilPkgIdent := ast.NewIdent("NewSet")
	info.Uses[nilPkgIdent] = types.NewFunc(token.NoPos, nil, "NewSet", nil)
	call = &ast.CallExpr{Fun: nilPkgIdent}
	if _, errs := oc.processExpr(info, "example.com/p", call, ""); len(errs) == 0 {
		t.Fatal("expected nil package error")
	}

	otherPkg := types.NewPackage("example.com/other", "other")
	otherIdent := ast.NewIdent("NewSet")
	info.Uses[otherIdent] = types.NewFunc(token.NoPos, otherPkg, "NewSet", nil)
	call = &ast.CallExpr{Fun: otherIdent}
	if _, errs := oc.processExpr(info, "example.com/p", call, ""); len(errs) == 0 {
		t.Fatal("expected non-wire package error")
	}

	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wireIdent := ast.NewIdent("Unknown")
	info.Uses[wireIdent] = types.NewFunc(token.NoPos, wirePkg, "Unknown", nil)
	call = &ast.CallExpr{Fun: wireIdent}
	if _, errs := oc.processExpr(info, "example.com/p", call, ""); len(errs) == 0 {
		t.Fatal("expected unknown wire function error")
	}
}

func TestInjectorFuncSignature(t *testing.T) {
	t.Parallel()

	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(), false)
	if _, _, err := injectorFuncSignature(sig); err == nil {
		t.Fatal("expected injector signature error")
	}

	results := types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]))
	sig = types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	if _, out, err := injectorFuncSignature(sig); err != nil || out.out == nil {
		t.Fatalf("expected injector signature, got=%+v err=%v", out, err)
	}
}

func TestProcessExprWireCalls(t *testing.T) {
	t.Parallel()

	oc := &objectCache{
		fset:     token.NewFileSet(),
		packages: make(map[string]*packages.Package),
		objects:  make(map[objRef]objCacheEntry),
		hasher:   typeutil.MakeHasher(),
	}
	info := &types.Info{
		Uses:  make(map[*ast.Ident]types.Object),
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wireIdent := ast.NewIdent("wire")
	info.Uses[wireIdent] = types.NewPkgName(token.NoPos, nil, "wire", wirePkg)

	valueIdent := ast.NewIdent("Value")
	info.Uses[valueIdent] = types.NewFunc(token.NoPos, wirePkg, "Value", nil)
	valueArg := &ast.BasicLit{Kind: token.INT, Value: "1"}
	info.Types[valueArg] = types.TypeAndValue{Type: types.Typ[types.Int]}
	valueCall := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: wireIdent, Sel: valueIdent},
		Args: []ast.Expr{valueArg},
	}
	if got, errs := oc.processExpr(info, "example.com/p", valueCall, ""); len(errs) > 0 || got == nil {
		t.Fatalf("expected value provider, got=%T errs=%v", got, errs)
	}

	ifaceIdent := ast.NewIdent("InterfaceValue")
	info.Uses[ifaceIdent] = types.NewFunc(token.NoPos, wirePkg, "InterfaceValue", nil)
	iface := types.NewInterfaceType(nil, nil)
	iface.Complete()
	ifaceArg := ast.NewIdent("iface")
	info.Types[ifaceArg] = types.TypeAndValue{Type: types.NewPointer(iface)}
	ifaceValue := &ast.BasicLit{Kind: token.INT, Value: "2"}
	info.Types[ifaceValue] = types.TypeAndValue{Type: types.Typ[types.Int]}
	ifaceCall := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: wireIdent, Sel: ifaceIdent},
		Args: []ast.Expr{ifaceArg, ifaceValue},
	}
	if got, errs := oc.processExpr(info, "example.com/p", ifaceCall, ""); len(errs) > 0 || got == nil {
		t.Fatalf("expected interface value, got=%T errs=%v", got, errs)
	}

	pkg := types.NewPackage("example.com/p", "p")
	typeName := types.NewTypeName(token.NoPos, pkg, "Foo", nil)
	named := types.NewNamed(typeName, types.NewStruct(nil, nil), nil)
	ptr := types.NewPointer(named)
	typeIdent := ast.NewIdent("Foo")
	info.Uses[typeIdent] = typeName
	newCall := &ast.CallExpr{Fun: ast.NewIdent("new"), Args: []ast.Expr{typeIdent}}
	info.Types[newCall] = types.TypeAndValue{Type: ptr}
	structIdent := ast.NewIdent("Struct")
	info.Uses[structIdent] = types.NewFunc(token.NoPos, wirePkg, "Struct", nil)
	structCall := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: wireIdent, Sel: structIdent},
		Args: []ast.Expr{newCall, &ast.BasicLit{Kind: token.STRING, Value: "\"*\""}},
	}
	if got, errs := oc.processExpr(info, "example.com/p", structCall, ""); len(errs) > 0 || got == nil {
		t.Fatalf("expected struct provider, got=%T errs=%v", got, errs)
	}
}

func TestProcessExprStructLiteral(t *testing.T) {
	t.Parallel()

	oc := &objectCache{
		fset:     token.NewFileSet(),
		packages: make(map[string]*packages.Package),
		objects:  make(map[objRef]objCacheEntry),
		hasher:   typeutil.MakeHasher(),
	}
	info := &types.Info{
		Uses:  make(map[*ast.Ident]types.Object),
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	pkg := types.NewPackage("example.com/p", "p")
	typeName := types.NewTypeName(token.NoPos, pkg, "Lit", nil)
	named := types.NewNamed(typeName, types.NewStruct(nil, nil), nil)
	typeIdent := ast.NewIdent("Lit")
	info.Uses[typeIdent] = typeName
	lit := &ast.CompositeLit{Type: typeIdent}
	info.Types[lit] = types.TypeAndValue{Type: named}
	if got, errs := oc.processExpr(info, pkg.Path(), lit, ""); len(errs) > 0 || got == nil {
		t.Fatalf("expected struct literal provider, got=%T errs=%v", got, errs)
	}
}
