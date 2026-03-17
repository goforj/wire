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

	"github.com/goforj/wire/internal/semanticcache"
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

func TestSummarizeSemanticProviderSet(t *testing.T) {
	t.Parallel()

	info := &types.Info{
		Uses: make(map[*ast.Ident]types.Object),
	}
	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wireIdent := ast.NewIdent("wire")
	newSetIdent := ast.NewIdent("NewSet")
	info.Uses[wireIdent] = types.NewPkgName(token.NoPos, nil, "wire", wirePkg)
	info.Uses[newSetIdent] = types.NewFunc(token.NoPos, wirePkg, "NewSet", nil)

	depPkg := types.NewPackage("example.com/dep", "dep")
	fnIdent := ast.NewIdent("NewMessage")
	info.Uses[fnIdent] = types.NewFunc(token.NoPos, depPkg, "NewMessage", types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(token.NoPos, depPkg, "", types.Typ[types.String])), false))

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{X: wireIdent, Sel: newSetIdent},
		Args: []ast.Expr{
			fnIdent,
		},
	}
	got, ok := summarizeSemanticProviderSet(info, call, "example.com/app")
	if !ok {
		t.Fatal("summarizeSemanticProviderSet() = unsupported, want supported")
	}
	if len(got.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(got.Items))
	}
	if got.Items[0].Kind != "func" || got.Items[0].ImportPath != "example.com/dep" || got.Items[0].Name != "NewMessage" {
		t.Fatalf("unexpected item: %+v", got.Items[0])
	}
}

func TestSummarizeSemanticProviderSetTypeOnlyForms(t *testing.T) {
	t.Parallel()

	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wireIdent := ast.NewIdent("wire")
	info.Uses[wireIdent] = types.NewPkgName(token.NoPos, nil, "wire", wirePkg)

	appPkg := types.NewPackage("example.com/app", "app")
	fooObj := types.NewTypeName(token.NoPos, appPkg, "Foo", nil)
	fooNamed := types.NewNamed(fooObj, types.NewStruct([]*types.Var{
		types.NewVar(token.NoPos, appPkg, "Message", types.Typ[types.String]),
	}, []string{""}), nil)
	fooIfaceObj := types.NewTypeName(token.NoPos, appPkg, "Fooer", nil)
	fooIface := types.NewNamed(fooIfaceObj, types.NewInterfaceType(nil, nil).Complete(), nil)

	newSetIdent := ast.NewIdent("NewSet")
	bindIdent := ast.NewIdent("Bind")
	structIdent := ast.NewIdent("Struct")
	fieldsIdent := ast.NewIdent("FieldsOf")
	info.Uses[newSetIdent] = types.NewFunc(token.NoPos, wirePkg, "NewSet", nil)
	info.Uses[bindIdent] = types.NewFunc(token.NoPos, wirePkg, "Bind", nil)
	info.Uses[structIdent] = types.NewFunc(token.NoPos, wirePkg, "Struct", nil)
	info.Uses[fieldsIdent] = types.NewFunc(token.NoPos, wirePkg, "FieldsOf", nil)

	newFooCall := &ast.CallExpr{Fun: ast.NewIdent("new"), Args: []ast.Expr{ast.NewIdent("Foo")}}
	info.Types[newFooCall] = types.TypeAndValue{Type: types.NewPointer(fooNamed)}
	newFooIfaceCall := &ast.CallExpr{Fun: ast.NewIdent("new"), Args: []ast.Expr{ast.NewIdent("Fooer")}}
	info.Types[newFooIfaceCall] = types.TypeAndValue{Type: types.NewPointer(fooIface)}
	ptrToPtrFooCall := &ast.CallExpr{Fun: ast.NewIdent("new"), Args: []ast.Expr{ast.NewIdent("FooPtr")}}
	info.Types[ptrToPtrFooCall] = types.TypeAndValue{Type: types.NewPointer(types.NewPointer(fooNamed))}

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{X: wireIdent, Sel: newSetIdent},
		Args: []ast.Expr{
			&ast.CallExpr{Fun: &ast.SelectorExpr{X: wireIdent, Sel: bindIdent}, Args: []ast.Expr{newFooIfaceCall, newFooCall}},
			&ast.CallExpr{Fun: &ast.SelectorExpr{X: wireIdent, Sel: structIdent}, Args: []ast.Expr{newFooCall, &ast.BasicLit{Kind: token.STRING, Value: "\"*\""}}},
			&ast.CallExpr{Fun: &ast.SelectorExpr{X: wireIdent, Sel: fieldsIdent}, Args: []ast.Expr{ptrToPtrFooCall, &ast.BasicLit{Kind: token.STRING, Value: "\"Message\""}}},
		},
	}
	if got, ok := summarizeSemanticProviderSet(info, call, "example.com/app"); ok || len(got.Items) != 0 {
		t.Fatalf("summarizeSemanticProviderSet(bind case) = (%+v, %v), want unsupported", got, ok)
	}

	call = &ast.CallExpr{
		Fun: &ast.SelectorExpr{X: wireIdent, Sel: newSetIdent},
		Args: []ast.Expr{
			&ast.CallExpr{Fun: &ast.SelectorExpr{X: wireIdent, Sel: structIdent}, Args: []ast.Expr{newFooCall, &ast.BasicLit{Kind: token.STRING, Value: "\"*\""}}},
			&ast.CallExpr{Fun: &ast.SelectorExpr{X: wireIdent, Sel: fieldsIdent}, Args: []ast.Expr{ptrToPtrFooCall, &ast.BasicLit{Kind: token.STRING, Value: "\"Message\""}}},
		},
	}
	got, ok := summarizeSemanticProviderSet(info, call, "example.com/app")
	if !ok {
		t.Fatal("summarizeSemanticProviderSet(non-bind type-only forms) = unsupported, want supported")
	}
	if len(got.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(got.Items))
	}
	if got.Items[0].Kind != "struct" || got.Items[1].Kind != "fields" {
		t.Fatalf("unexpected kinds: %+v", got.Items)
	}
}

func TestObjectCacheSemanticProviderSetFallback(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wireObj := types.NewTypeName(token.NoPos, wirePkg, "ProviderSet", nil)
	wireNamed := types.NewNamed(wireObj, types.NewStruct(nil, nil), nil)

	depTypes := types.NewPackage("example.com/dep", "dep")
	msgFnSig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(token.NoPos, depTypes, "", types.Typ[types.String])), false)
	msgFn := types.NewFunc(token.NoPos, depTypes, "NewMessage", msgFnSig)
	setVar := types.NewVar(token.NoPos, depTypes, "Set", wireNamed)
	depTypes.Scope().Insert(msgFn)
	depTypes.Scope().Insert(setVar)

	depPkg := &packages.Package{
		Name:    "dep",
		PkgPath: depTypes.Path(),
		Types:   depTypes,
		Fset:    fset,
		Imports: make(map[string]*packages.Package),
	}
	oc := &objectCache{
		fset:     fset,
		packages: map[string]*packages.Package{depPkg.PkgPath: depPkg},
		objects:  make(map[objRef]objCacheEntry),
		semantic: map[string]*semanticcache.PackageArtifact{
			depPkg.PkgPath: {
				Version:     1,
				PackagePath: depPkg.PkgPath,
				PackageName: depPkg.Name,
				Supported:   true,
				Vars: map[string]semanticcache.ProviderSetArtifact{
					"Set": {
						Items: []semanticcache.ProviderSetItemArtifact{
							{Kind: "func", ImportPath: depPkg.PkgPath, Name: "NewMessage"},
						},
					},
				},
			},
		},
		hasher: typeutil.MakeHasher(),
	}
	item, errs := oc.get(setVar)
	if len(errs) > 0 {
		t.Fatalf("oc.get(Set) errs = %v", errs)
	}
	pset, ok := item.(*ProviderSet)
	if !ok || pset == nil {
		t.Fatalf("oc.get(Set) type = %T, want *ProviderSet", item)
	}
	if len(pset.Providers) != 1 || pset.Providers[0].Name != "NewMessage" {
		t.Fatalf("unexpected providers: %+v", pset.Providers)
	}
}

func TestObjectCacheSemanticProviderSetSkipsBindArtifacts(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wireObj := types.NewTypeName(token.NoPos, wirePkg, "ProviderSet", nil)
	wireNamed := types.NewNamed(wireObj, types.NewStruct(nil, nil), nil)

	appTypes := types.NewPackage("example.com/app", "app")
	fooIfaceObj := types.NewTypeName(token.NoPos, appTypes, "Fooer", nil)
	_ = types.NewNamed(fooIfaceObj, types.NewInterfaceType(nil, nil).Complete(), nil)
	fooObj := types.NewTypeName(token.NoPos, appTypes, "Foo", nil)
	_ = types.NewNamed(fooObj, types.NewStruct([]*types.Var{
		types.NewVar(token.NoPos, appTypes, "Message", types.Typ[types.String]),
	}, []string{""}), nil)
	setVar := types.NewVar(token.NoPos, appTypes, "Set", wireNamed)
	appTypes.Scope().Insert(fooIfaceObj)
	appTypes.Scope().Insert(fooObj)
	appTypes.Scope().Insert(setVar)

	appPkg := &packages.Package{
		Name:    "app",
		PkgPath: appTypes.Path(),
		Types:   appTypes,
		Fset:    fset,
		Imports: make(map[string]*packages.Package),
	}
	oc := &objectCache{
		fset:     fset,
		packages: map[string]*packages.Package{appPkg.PkgPath: appPkg},
		objects:  make(map[objRef]objCacheEntry),
		semantic: map[string]*semanticcache.PackageArtifact{
			appPkg.PkgPath: {
				Version:     1,
				PackagePath: appPkg.PkgPath,
				PackageName: appPkg.Name,
				Supported:   true,
				Vars: map[string]semanticcache.ProviderSetArtifact{
					"Set": {
						Items: []semanticcache.ProviderSetItemArtifact{
							{
								Kind:  "bind",
								Type:  semanticcache.TypeRef{ImportPath: appPkg.PkgPath, Name: "Fooer"},
								Type2: semanticcache.TypeRef{ImportPath: appPkg.PkgPath, Name: "Foo"},
							},
							{
								Kind:      "struct",
								Type:      semanticcache.TypeRef{ImportPath: appPkg.PkgPath, Name: "Foo"},
								AllFields: true,
							},
							{
								Kind:       "fields",
								Type:       semanticcache.TypeRef{ImportPath: appPkg.PkgPath, Name: "Foo", Pointer: 2},
								FieldNames: []string{"Message"},
							},
						},
					},
				},
			},
		},
		hasher: typeutil.MakeHasher(),
	}
	pset, ok, errs := oc.semanticProviderSet(setVar)
	if len(errs) > 0 {
		t.Fatalf("semanticProviderSet(Set) errs = %v", errs)
	}
	if ok {
		t.Fatalf("semanticProviderSet(Set) ok = true, want false")
	}
	if pset != nil {
		t.Fatalf("semanticProviderSet(Set) = %#v, want nil", pset)
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

func TestNewObjectCacheRegistersPackages(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	pkg := &packages.Package{PkgPath: "example.com/p", Fset: fset}
	oc := newObjectCache([]*packages.Package{pkg})

	if got := oc.packages[pkg.PkgPath]; got != pkg {
		t.Fatalf("expected package to be registered, got %v", got)
	}
	if got := oc.packages["missing.example.com"]; got != nil {
		t.Fatalf("expected missing package to remain absent, got %v", got)
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
