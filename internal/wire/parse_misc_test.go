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

	"golang.org/x/tools/go/types/typeutil"
)

func TestProviderSetOutputs(t *testing.T) {
	set := &ProviderSet{
		providerMap: &typeutil.Map{},
	}
	provided := &ProvidedType{t: types.Typ[types.Int]}
	set.providerMap.Set(types.Typ[types.Int], provided)
	outputs := set.Outputs()
	if len(outputs) != 1 || outputs[0] != types.Typ[types.Int] {
		t.Fatalf("unexpected outputs: %v", outputs)
	}
}

func TestProviderSetIDString(t *testing.T) {
	id := ProviderSetID{
		ImportPath: "example.com/pkg",
		VarName:    "Set",
	}
	if got := id.String(); got != "\"example.com/pkg\".Set" {
		t.Fatalf("unexpected ProviderSetID string: %q", got)
	}
}

func TestInjectorString(t *testing.T) {
	inj := &Injector{
		ImportPath: "example.com/pkg",
		FuncName:   "Init",
	}
	if got := inj.String(); got != "\"example.com/pkg\".Init" {
		t.Fatalf("unexpected Injector string: %q", got)
	}
}

func TestStructArgType(t *testing.T) {
	pkg := types.NewPackage("example.com/p", "p")
	obj := types.NewTypeName(token.NoPos, pkg, "S", nil)
	named := types.NewNamed(obj, types.NewStruct(nil, nil), nil)
	_ = named

	ident := &ast.Ident{Name: "S"}
	info := &types.Info{
		Uses: map[*ast.Ident]types.Object{
			ident: obj,
		},
	}
	lit := &ast.CompositeLit{Type: ident}
	if got := structArgType(info, lit); got != obj {
		t.Fatalf("expected struct type, got %v", got)
	}

	nonStructObj := types.NewTypeName(token.NoPos, pkg, "N", types.Typ[types.Int])
	info.Uses[ident] = nonStructObj
	if got := structArgType(info, lit); got != nil {
		t.Fatalf("expected nil for non-struct, got %v", got)
	}

	if got := structArgType(info, &ast.BasicLit{}); got != nil {
		t.Fatalf("expected nil for non composite literal, got %v", got)
	}
}

func TestProcessStructLiteralProvider(t *testing.T) {
	fset := token.NewFileSet()
	file := fset.AddFile("provider.go", -1, 100)
	pos := file.Pos(1)
	pkg := types.NewPackage("example.com/p", "p")

	fields := []*types.Var{
		types.NewVar(pos, pkg, "A", types.Typ[types.Int]),
		types.NewVar(pos, pkg, "B", types.Typ[types.String]),
	}
	obj := types.NewTypeName(pos, pkg, "S", nil)
	named := types.NewNamed(obj, types.NewStruct(fields, nil), nil)
	_ = named

	provider, errs := processStructLiteralProvider(fset, obj)
	if len(errs) > 0 || provider == nil {
		t.Fatalf("expected provider, got errs=%v", errs)
	}
	if len(provider.Out) != 2 {
		t.Fatalf("expected pointer and value outputs, got %d", len(provider.Out))
	}

	dupFields := []*types.Var{
		types.NewVar(pos, pkg, "A", types.Typ[types.Int]),
		types.NewVar(pos, pkg, "B", types.Typ[types.Int]),
	}
	dupObj := types.NewTypeName(pos, pkg, "D", nil)
	dupNamed := types.NewNamed(dupObj, types.NewStruct(dupFields, nil), nil)
	_ = dupNamed
	if _, errs := processStructLiteralProvider(fset, dupObj); len(errs) == 0 {
		t.Fatal("expected duplicate field type error")
	}

	nonStructObj := types.NewTypeName(pos, pkg, "N", types.Typ[types.Int])
	if _, errs := processStructLiteralProvider(fset, nonStructObj); len(errs) == 0 {
		t.Fatal("expected non-struct error")
	}
}

func TestProcessValue(t *testing.T) {
	fset := token.NewFileSet()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	call := &ast.CallExpr{Fun: &ast.Ident{Name: "Value"}, Args: []ast.Expr{}}
	if _, err := processValue(fset, info, call); err == nil {
		t.Fatal("expected argument count error")
	}

	arg := &ast.BasicLit{}
	call = &ast.CallExpr{Fun: &ast.Ident{Name: "Value"}, Args: []ast.Expr{arg}}
	info.Types[arg] = types.TypeAndValue{Type: types.Typ[types.Int]}
	if _, err := processValue(fset, info, call); err != nil {
		t.Fatalf("expected basic value, got %v", err)
	}

	unary := &ast.UnaryExpr{Op: token.ARROW}
	call = &ast.CallExpr{Fun: &ast.Ident{Name: "Value"}, Args: []ast.Expr{unary}}
	info.Types[unary] = types.TypeAndValue{Type: types.Typ[types.Int]}
	if _, err := processValue(fset, info, call); err == nil {
		t.Fatal("expected unary arrow error")
	}

	fnIdent := &ast.Ident{Name: "f"}
	fnCall := &ast.CallExpr{Fun: fnIdent}
	info.Types[fnIdent] = types.TypeAndValue{Type: types.NewSignatureType(nil, nil, nil, nil, nil, false)}
	info.Types[fnCall] = types.TypeAndValue{Type: types.Typ[types.Int]}
	call = &ast.CallExpr{Fun: &ast.Ident{Name: "Value"}, Args: []ast.Expr{fnCall}}
	if _, err := processValue(fset, info, call); err == nil {
		t.Fatal("expected func call error")
	}

	iface := types.NewInterfaceType(nil, nil)
	iface.Complete()
	ident := &ast.Ident{Name: "i"}
	info.Types[ident] = types.TypeAndValue{Type: iface}
	call = &ast.CallExpr{Fun: &ast.Ident{Name: "Value"}, Args: []ast.Expr{ident}}
	if _, err := processValue(fset, info, call); err == nil {
		t.Fatal("expected interface type error")
	}
}

func TestProcessInterfaceValue(t *testing.T) {
	fset := token.NewFileSet()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	call := &ast.CallExpr{Fun: &ast.Ident{Name: "InterfaceValue"}, Args: []ast.Expr{}}
	if _, err := processInterfaceValue(fset, info, call); err == nil {
		t.Fatal("expected arg count error")
	}

	first := &ast.Ident{Name: "first"}
	second := &ast.Ident{Name: "second"}
	call.Args = []ast.Expr{first, second}
	info.Types[first] = types.TypeAndValue{Type: types.Typ[types.Int]}
	info.Types[second] = types.TypeAndValue{Type: types.Typ[types.Int]}
	if _, err := processInterfaceValue(fset, info, call); err == nil {
		t.Fatal("expected non-pointer interface error")
	}

	ptrToInt := types.NewPointer(types.Typ[types.Int])
	info.Types[first] = types.TypeAndValue{Type: ptrToInt}
	if _, err := processInterfaceValue(fset, info, call); err == nil {
		t.Fatal("expected pointer to non-interface error")
	}

	iface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, nil, "M", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	}, nil)
	iface.Complete()
	info.Types[first] = types.TypeAndValue{Type: types.NewPointer(iface)}
	info.Types[second] = types.TypeAndValue{Type: types.Typ[types.Int]}
	if _, err := processInterfaceValue(fset, info, call); err == nil {
		t.Fatal("expected implement error")
	}

	emptyIface := types.NewInterfaceType(nil, nil)
	emptyIface.Complete()
	info.Types[first] = types.TypeAndValue{Type: types.NewPointer(emptyIface)}
	info.Types[second] = types.TypeAndValue{Type: types.Typ[types.Int]}
	if _, err := processInterfaceValue(fset, info, call); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestProcessFieldsOf(t *testing.T) {
	fset := token.NewFileSet()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	call := &ast.CallExpr{Fun: &ast.Ident{Name: "FieldsOf"}, Args: []ast.Expr{}}
	if _, err := processFieldsOf(fset, info, call); err == nil {
		t.Fatal("expected arg count error")
	}

	first := &ast.Ident{Name: "first"}
	call.Args = []ast.Expr{first, &ast.BasicLit{Value: "\"A\""}}
	info.Types[first] = types.TypeAndValue{Type: types.Typ[types.Int]}
	if _, err := processFieldsOf(fset, info, call); err == nil {
		t.Fatal("expected non-pointer error")
	}

	ptrToInt := types.NewPointer(types.Typ[types.Int])
	info.Types[first] = types.TypeAndValue{Type: ptrToInt}
	if _, err := processFieldsOf(fset, info, call); err == nil {
		t.Fatal("expected pointer to non-struct error")
	}

	fields := []*types.Var{
		types.NewVar(token.NoPos, nil, "A", types.Typ[types.Int]),
	}
	tags := []string{`wire:"-"`}
	st := types.NewStruct(fields, tags)
	info.Types[first] = types.TypeAndValue{Type: types.NewPointer(st)}
	if _, err := processFieldsOf(fset, info, call); err == nil {
		t.Fatal("expected prevented field error")
	}

	info.Types[first] = types.TypeAndValue{Type: types.NewPointer(st)}
	call.Args = []ast.Expr{first, &ast.BasicLit{Value: "\"B\""}}
	if _, err := processFieldsOf(fset, info, call); err == nil {
		t.Fatal("expected missing field error")
	}

	call.Args = []ast.Expr{first, &ast.BasicLit{Value: "\"A\""}, &ast.BasicLit{Value: "\"A\""}}
	if _, err := processFieldsOf(fset, info, call); err == nil {
		t.Fatal("expected field count error")
	}

	st2 := types.NewStruct([]*types.Var{
		types.NewVar(token.NoPos, nil, "A", types.Typ[types.Int]),
	}, []string{""})
	info.Types[first] = types.TypeAndValue{Type: types.NewPointer(st2)}
	call.Args = []ast.Expr{first, &ast.BasicLit{Value: "\"A\""}}
	if fields, err := processFieldsOf(fset, info, call); err != nil || len(fields) != 1 {
		t.Fatalf("expected fields, got %v err=%v", fields, err)
	}

	ptrToPtr := types.NewPointer(types.NewPointer(st2))
	info.Types[first] = types.TypeAndValue{Type: ptrToPtr}
	if fields, err := processFieldsOf(fset, info, call); err != nil || len(fields[0].Out) != 2 {
		t.Fatalf("expected pointer fields, got %v err=%v", fields, err)
	}
}

func TestProcessBind(t *testing.T) {
	fset := token.NewFileSet()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	pkg := types.NewPackage("example.com/p", "p")
	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wirePkg.Scope().Insert(types.NewVar(token.NoPos, wirePkg, "bindToUsePointer", types.Typ[types.Int]))
	wireIdent := &ast.Ident{Name: "wire"}
	info.Uses[wireIdent] = types.NewPkgName(token.NoPos, nil, "wire", wirePkg)

	call := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: wireIdent, Sel: &ast.Ident{Name: "Bind"}},
		Args: []ast.Expr{},
	}
	if _, err := processBind(fset, info, call); err == nil {
		t.Fatal("expected arg count error")
	}

	ifaceIdent := &ast.Ident{Name: "iface"}
	provIdent := &ast.Ident{Name: "prov"}
	call.Args = []ast.Expr{ifaceIdent, provIdent}
	info.Types[ifaceIdent] = types.TypeAndValue{Type: types.Typ[types.Int]}
	info.Types[provIdent] = types.TypeAndValue{Type: types.Typ[types.Int]}
	if _, err := processBind(fset, info, call); err == nil {
		t.Fatal("expected iface pointer error")
	}

	ptrToInt := types.NewPointer(types.Typ[types.Int])
	info.Types[ifaceIdent] = types.TypeAndValue{Type: ptrToInt}
	if _, err := processBind(fset, info, call); err == nil {
		t.Fatal("expected pointer to non-interface error")
	}

	iface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "M", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	}, nil)
	iface.Complete()
	info.Types[ifaceIdent] = types.TypeAndValue{Type: types.NewPointer(iface)}
	info.Types[provIdent] = types.TypeAndValue{Type: types.Typ[types.Int]}
	if _, err := processBind(fset, info, call); err == nil {
		t.Fatal("expected pointer requirement error")
	}

	info.Types[provIdent] = types.TypeAndValue{Type: types.NewPointer(types.Typ[types.Int])}
	if _, err := processBind(fset, info, call); err == nil {
		t.Fatal("expected implements error")
	}

	emptyIface := types.NewInterfaceType(nil, nil)
	emptyIface.Complete()
	info.Types[ifaceIdent] = types.TypeAndValue{Type: types.NewPointer(emptyIface)}
	info.Types[provIdent] = types.TypeAndValue{Type: types.NewPointer(types.Typ[types.Int])}
	if _, err := processBind(fset, info, call); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestProvidedTypeAccessors(t *testing.T) {
	typ := types.Typ[types.Int]
	provider := &Provider{}
	value := &Value{}
	arg := &InjectorArg{}
	field := &Field{}

	assertPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("expected panic for %s", name)
			}
		}()
		fn()
	}
	zero := ProvidedType{}
	assertPanic("Provider", func() { zero.Provider() })
	assertPanic("Value", func() { zero.Value() })
	assertPanic("Arg", func() { zero.Arg() })
	assertPanic("Field", func() { zero.Field() })
	if got := (ProvidedType{t: typ, p: provider}).Provider(); got != provider {
		t.Fatal("expected provider")
	}
	if got := (ProvidedType{t: typ, v: value}).Value(); got != value {
		t.Fatal("expected value")
	}
	if got := (ProvidedType{t: typ, a: arg}).Arg(); got != arg {
		t.Fatal("expected arg")
	}
	if got := (ProvidedType{t: typ, f: field}).Field(); got != field {
		t.Fatal("expected field")
	}
}

func TestProviderSetSrcDescription(t *testing.T) {
	fset := token.NewFileSet()
	file := fset.AddFile("src.go", -1, 100)
	pos := file.Pos(1)
	pkg := types.NewPackage("example.com/p", "p")
	s := &providerSetSrc{
		Provider: &Provider{
			Pkg:      pkg,
			Name:     "Make",
			Pos:      pos,
			IsStruct: false,
		},
	}
	if got := s.description(fset, types.Typ[types.Int]); got == "" {
		t.Fatal("expected provider description")
	}
	s.Provider.IsStruct = true
	if got := s.description(fset, types.Typ[types.Int]); got == "" {
		t.Fatal("expected struct provider description")
	}
	s.Provider = nil
	s.Binding = &IfaceBinding{Pos: pos}
	if got := s.description(fset, types.Typ[types.Int]); got == "" {
		t.Fatal("expected binding description")
	}
	s.Binding = nil
	s.Value = &Value{Pos: pos}
	if got := s.description(fset, types.Typ[types.Int]); got == "" {
		t.Fatal("expected value description")
	}
	s.Value = nil
	s.Import = &ProviderSet{VarName: "Set", Pos: pos, srcMap: typeutilMakeMap()}
	if got := s.description(fset, types.Typ[types.Int]); got == "" {
		t.Fatal("expected import description")
	}
	s.Import = nil
	s.InjectorArg = &InjectorArg{Index: 0, Args: &InjectorArgs{Name: "Init", Pos: pos, Tuple: types.NewTuple(types.NewVar(pos, pkg, "arg", types.Typ[types.Int]))}}
	if got := s.description(fset, types.Typ[types.Int]); got == "" {
		t.Fatal("expected injector arg description")
	}
	s.InjectorArg = nil
	s.Field = &Field{Pos: pos}
	if got := s.description(fset, types.Typ[types.Int]); got == "" {
		t.Fatal("expected field description")
	}
}

func TestIsProviderSetType(t *testing.T) {
	if isProviderSetType(types.Typ[types.Int]) {
		t.Fatal("expected false for non-named type")
	}
	pkg := types.NewPackage("example.com/p", "p")
	obj := types.NewTypeName(token.NoPos, pkg, "Other", nil)
	named := types.NewNamed(obj, types.Typ[types.Int], nil)
	if isProviderSetType(named) {
		t.Fatal("expected false for non ProviderSet name")
	}
	wirePkg := types.NewPackage("github.com/goforj/wire", "wire")
	wireObj := types.NewTypeName(token.NoPos, wirePkg, "ProviderSet", nil)
	wireNamed := types.NewNamed(wireObj, types.Typ[types.Int], nil)
	if !isProviderSetType(wireNamed) {
		t.Fatal("expected true for ProviderSet")
	}
}

func typeutilMakeMap() *typeutil.Map {
	return &typeutil.Map{}
}
