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
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/packages"
)

const incrementalSummaryVersion = "wire-incremental-summary-v1"

type packageSummary struct {
	Version      string
	WD           string
	Tags         string
	PkgPath      string
	ShapeHash    string
	LocalImports []string
	ProviderSets []providerSetSummary
	Injectors    []injectorSummary
}

type providerSetSummary struct {
	VarName    string
	Providers  []providerSummary
	Imports    []providerSetRefSummary
	Bindings   []ifaceBindingSummary
	Values     []string
	Fields     []fieldSummary
	InputTypes []string
}

type providerSummary struct {
	PkgPath    string
	Name       string
	Args       []providerInputSummary
	Out        []string
	Varargs    bool
	IsStruct   bool
	HasCleanup bool
	HasErr     bool
}

type providerInputSummary struct {
	Type      string
	FieldName string
}

type providerSetRefSummary struct {
	PkgPath string
	VarName string
}

type ifaceBindingSummary struct {
	Iface    string
	Provided string
}

type fieldSummary struct {
	PkgPath string
	Parent  string
	Name    string
	Out     []string
}

type injectorSummary struct {
	Name   string
	Inputs []string
	Output string
	Build  providerSetSummary
}

type packageSummarySnapshot struct {
	Changed   map[string]*packageSummary
	Unchanged map[string]*packageSummary
}

func incrementalSummaryKey(wd string, tags string, pkgPath string) string {
	h := sha256.New()
	h.Write([]byte(incrementalSummaryVersion))
	h.Write([]byte{0})
	h.Write([]byte(packageCacheScope(wd)))
	h.Write([]byte{0})
	h.Write([]byte(tags))
	h.Write([]byte{0})
	h.Write([]byte(pkgPath))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func incrementalSummaryPath(key string) string {
	return filepath.Join(cacheDir(), key+".isum")
}

func readIncrementalPackageSummary(key string) (*packageSummary, bool) {
	data, err := osReadFile(incrementalSummaryPath(key))
	if err != nil {
		return nil, false
	}
	summary, err := decodeIncrementalSummary(data)
	if err != nil {
		return nil, false
	}
	return summary, true
}

func writeIncrementalPackageSummary(key string, summary *packageSummary) {
	data, err := encodeIncrementalSummary(summary)
	if err != nil {
		return
	}
	dir := cacheDir()
	if err := osMkdirAll(dir, 0755); err != nil {
		return
	}
	tmp, err := osCreateTemp(dir, key+".isum-")
	if err != nil {
		return
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		osRemove(tmp.Name())
		return
	}
	if err := osRename(tmp.Name(), incrementalSummaryPath(key)); err != nil {
		osRemove(tmp.Name())
	}
}

func writeIncrementalPackageSummaries(loader *lazyLoader, pkgs []*packages.Package) {
	writeIncrementalPackageSummariesWithSummary(loader, pkgs, nil, nil)
}

func writeIncrementalPackageSummariesWithSummary(loader *lazyLoader, pkgs []*packages.Package, summary *summaryProviderResolver, only map[string]struct{}) {
	if loader == nil || len(pkgs) == 0 {
		return
	}
	moduleRoot := findModuleRoot(loader.wd)
	all := collectAllPackages(pkgs)
	for path, pkg := range loader.loaded {
		if pkg != nil {
			all[path] = pkg
		}
	}
	allPkgs := make([]*packages.Package, 0, len(all))
	for _, pkg := range all {
		allPkgs = append(allPkgs, pkg)
	}
	oc := newObjectCacheWithLoader(allPkgs, loader, nil, summary)
	for _, pkg := range all {
		if classifyPackageLocation(moduleRoot, pkg) != "local" {
			continue
		}
		if len(only) > 0 {
			if _, ok := only[pkg.PkgPath]; !ok {
				continue
			}
		}
		if pkg == nil || pkg.TypesInfo == nil || len(pkg.Syntax) == 0 {
			continue
		}
		summary, err := buildPackageSummary(loader, oc, pkg)
		if err != nil {
			continue
		}
		writeIncrementalPackageSummary(incrementalSummaryKey(loader.wd, loader.tags, pkg.PkgPath), summary)
	}
}

func collectIncrementalPackageSummaries(loader *lazyLoader, pkgs []*packages.Package) *packageSummarySnapshot {
	if loader == nil || loader.fingerprints == nil {
		return nil
	}
	snapshot := &packageSummarySnapshot{
		Changed:   make(map[string]*packageSummary),
		Unchanged: make(map[string]*packageSummary),
	}
	changed := make(map[string]struct{}, len(loader.fingerprints.changed))
	for _, path := range loader.fingerprints.changed {
		changed[path] = struct{}{}
	}
	moduleRoot := findModuleRoot(loader.wd)
	oc := newObjectCache(pkgs, loader)
	for _, pkg := range collectAllPackages(pkgs) {
		if pkg == nil {
			continue
		}
		if classifyPackageLocation(moduleRoot, pkg) != "local" {
			continue
		}
		if _, ok := changed[pkg.PkgPath]; ok {
			if pkg.TypesInfo == nil || len(pkg.Syntax) == 0 {
				loaded, errs := oc.ensurePackage(pkg.PkgPath)
				if len(errs) > 0 {
					continue
				}
				pkg = loaded
			}
			if pkg == nil || pkg.TypesInfo == nil || len(pkg.Syntax) == 0 {
				continue
			}
			summary, err := buildPackageSummary(loader, oc, pkg)
			if err != nil {
				continue
			}
			snapshot.Changed[pkg.PkgPath] = summary
			continue
		}
		if summary, ok := readIncrementalPackageSummary(incrementalSummaryKey(loader.wd, loader.tags, pkg.PkgPath)); ok {
			snapshot.Unchanged[pkg.PkgPath] = summary
		}
	}
	return snapshot
}

func buildPackageSummary(loader *lazyLoader, oc *objectCache, pkg *packages.Package) (*packageSummary, error) {
	if loader == nil || oc == nil || pkg == nil {
		return nil, fmt.Errorf("missing loader, object cache, or package")
	}
	summary := &packageSummary{
		Version: incrementalSummaryVersion,
		WD:      filepath.Clean(loader.wd),
		Tags:    loader.tags,
		PkgPath: pkg.PkgPath,
	}
	if snapshot := loader.fingerprints; snapshot != nil {
		if fp := snapshot.fingerprints[pkg.PkgPath]; fp != nil {
			summary.ShapeHash = fp.ShapeHash
			summary.LocalImports = append(summary.LocalImports, fp.LocalImports...)
		}
	}
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !isProviderSetType(obj.Type()) {
			continue
		}
		item, errs := oc.get(obj)
		if len(errs) > 0 {
			continue
		}
		pset, ok := item.(*ProviderSet)
		if !ok {
			continue
		}
		summary.ProviderSets = append(summary.ProviderSets, summarizeProviderSet(pset))
	}
	sort.Slice(summary.ProviderSets, func(i, j int) bool {
		return summary.ProviderSets[i].VarName < summary.ProviderSets[j].VarName
	})
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			buildCall, err := findInjectorBuild(pkg.TypesInfo, fn)
			if err != nil || buildCall == nil {
				continue
			}
			sig := pkg.TypesInfo.ObjectOf(fn.Name).Type().(*types.Signature)
			ins, out, err := injectorFuncSignature(sig)
			if err != nil {
				continue
			}
			injectorArgs := &InjectorArgs{
				Name:  fn.Name.Name,
				Tuple: ins,
				Pos:   fn.Pos(),
			}
			set, errs := oc.processNewSet(pkg.TypesInfo, pkg.PkgPath, buildCall, injectorArgs, "")
			if len(errs) > 0 {
				continue
			}
			summary.Injectors = append(summary.Injectors, injectorSummary{
				Name:   fn.Name.Name,
				Inputs: summarizeTuple(ins),
				Output: summaryTypeString(out.out),
				Build:  summarizeProviderSet(set),
			})
		}
	}
	sort.Slice(summary.Injectors, func(i, j int) bool {
		return summary.Injectors[i].Name < summary.Injectors[j].Name
	})
	return summary, nil
}

func summarizeProviderSet(pset *ProviderSet) providerSetSummary {
	if pset == nil {
		return providerSetSummary{}
	}
	summary := providerSetSummary{
		VarName: pset.VarName,
	}
	for _, provider := range pset.Providers {
		summary.Providers = append(summary.Providers, summarizeProvider(provider))
	}
	for _, imported := range pset.Imports {
		summary.Imports = append(summary.Imports, providerSetRefSummary{
			PkgPath: imported.PkgPath,
			VarName: imported.VarName,
		})
	}
	for _, binding := range pset.Bindings {
		summary.Bindings = append(summary.Bindings, ifaceBindingSummary{
			Iface:    summaryTypeString(binding.Iface),
			Provided: summaryTypeString(binding.Provided),
		})
	}
	for _, value := range pset.Values {
		summary.Values = append(summary.Values, summaryTypeString(value.Out))
	}
	for _, field := range pset.Fields {
		item := fieldSummary{
			Parent: summaryTypeString(field.Parent),
			Name:   field.Name,
			Out:    summarizeTypes(field.Out),
		}
		if field.Pkg != nil {
			item.PkgPath = field.Pkg.Path()
		}
		summary.Fields = append(summary.Fields, item)
	}
	if pset.InjectorArgs != nil {
		summary.InputTypes = summarizeTuple(pset.InjectorArgs.Tuple)
	}
	sort.Slice(summary.Providers, func(i, j int) bool {
		return summary.Providers[i].PkgPath+"."+summary.Providers[i].Name < summary.Providers[j].PkgPath+"."+summary.Providers[j].Name
	})
	sort.Slice(summary.Imports, func(i, j int) bool {
		return summary.Imports[i].PkgPath+"."+summary.Imports[i].VarName < summary.Imports[j].PkgPath+"."+summary.Imports[j].VarName
	})
	sort.Slice(summary.Bindings, func(i, j int) bool {
		return summary.Bindings[i].Iface+":"+summary.Bindings[i].Provided < summary.Bindings[j].Iface+":"+summary.Bindings[j].Provided
	})
	sort.Strings(summary.Values)
	sort.Slice(summary.Fields, func(i, j int) bool {
		return summary.Fields[i].Parent+"."+summary.Fields[i].Name < summary.Fields[j].Parent+"."+summary.Fields[j].Name
	})
	sort.Strings(summary.InputTypes)
	return summary
}

func summarizeProvider(provider *Provider) providerSummary {
	summary := providerSummary{
		Name:       provider.Name,
		Varargs:    provider.Varargs,
		IsStruct:   provider.IsStruct,
		HasCleanup: provider.HasCleanup,
		HasErr:     provider.HasErr,
		Out:        summarizeTypes(provider.Out),
	}
	if provider.Pkg != nil {
		summary.PkgPath = provider.Pkg.Path()
	}
	for _, arg := range provider.Args {
		summary.Args = append(summary.Args, providerInputSummary{
			Type:      summaryTypeString(arg.Type),
			FieldName: arg.FieldName,
		})
	}
	return summary
}

func summarizeTuple(tuple *types.Tuple) []string {
	if tuple == nil {
		return nil
	}
	out := make([]string, 0, tuple.Len())
	for i := 0; i < tuple.Len(); i++ {
		out = append(out, summaryTypeString(tuple.At(i).Type()))
	}
	return out
}

func summarizeTypes(typesList []types.Type) []string {
	out := make([]string, 0, len(typesList))
	for _, t := range typesList {
		out = append(out, summaryTypeString(t))
	}
	return out
}

func summaryTypeString(t types.Type) string {
	if t == nil {
		return ""
	}
	return types.TypeString(t, func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		return pkg.Path()
	})
}

func encodeIncrementalSummary(summary *packageSummary) ([]byte, error) {
	if summary == nil {
		return nil, fmt.Errorf("nil package summary")
	}
	var buf bytes.Buffer
	enc := binarySummaryEncoder{buf: &buf}
	enc.string(summary.Version)
	enc.string(summary.WD)
	enc.string(summary.Tags)
	enc.string(summary.PkgPath)
	enc.string(summary.ShapeHash)
	enc.strings(summary.LocalImports)
	enc.providerSets(summary.ProviderSets)
	enc.u32(uint32(len(summary.Injectors)))
	for _, injector := range summary.Injectors {
		enc.string(injector.Name)
		enc.strings(injector.Inputs)
		enc.string(injector.Output)
		enc.providerSet(injector.Build)
	}
	if enc.err != nil {
		return nil, enc.err
	}
	return buf.Bytes(), nil
}

func decodeIncrementalSummary(data []byte) (*packageSummary, error) {
	dec := binarySummaryDecoder{r: bytes.NewReader(data)}
	summary := &packageSummary{
		Version:   dec.string(),
		WD:        dec.string(),
		Tags:      dec.string(),
		PkgPath:   dec.string(),
		ShapeHash: dec.string(),
	}
	summary.LocalImports = dec.strings()
	summary.ProviderSets = dec.providerSets()
	for n := dec.u32(); n > 0; n-- {
		summary.Injectors = append(summary.Injectors, injectorSummary{
			Name:   dec.string(),
			Inputs: dec.strings(),
			Output: dec.string(),
			Build:  dec.providerSet(),
		})
	}
	if dec.err != nil {
		return nil, dec.err
	}
	return summary, nil
}

type binarySummaryEncoder struct {
	buf *bytes.Buffer
	err error
}

func (e *binarySummaryEncoder) u32(v uint32) {
	if e.err != nil {
		return
	}
	e.err = binary.Write(e.buf, binary.LittleEndian, v)
}

func (e *binarySummaryEncoder) string(s string) {
	e.u32(uint32(len(s)))
	if e.err != nil {
		return
	}
	_, e.err = e.buf.WriteString(s)
}

func (e *binarySummaryEncoder) bool(v bool) {
	if e.err != nil {
		return
	}
	var b byte
	if v {
		b = 1
	}
	e.err = e.buf.WriteByte(b)
}

func (e *binarySummaryEncoder) strings(values []string) {
	e.u32(uint32(len(values)))
	for _, v := range values {
		e.string(v)
	}
}

func (e *binarySummaryEncoder) providerSets(values []providerSetSummary) {
	e.u32(uint32(len(values)))
	for _, value := range values {
		e.providerSet(value)
	}
}

func (e *binarySummaryEncoder) providerSet(value providerSetSummary) {
	e.string(value.VarName)
	e.u32(uint32(len(value.Providers)))
	for _, provider := range value.Providers {
		e.string(provider.PkgPath)
		e.string(provider.Name)
		e.u32(uint32(len(provider.Args)))
		for _, arg := range provider.Args {
			e.string(arg.Type)
			e.string(arg.FieldName)
		}
		e.strings(provider.Out)
		e.bool(provider.Varargs)
		e.bool(provider.IsStruct)
		e.bool(provider.HasCleanup)
		e.bool(provider.HasErr)
	}
	e.u32(uint32(len(value.Imports)))
	for _, imported := range value.Imports {
		e.string(imported.PkgPath)
		e.string(imported.VarName)
	}
	e.u32(uint32(len(value.Bindings)))
	for _, binding := range value.Bindings {
		e.string(binding.Iface)
		e.string(binding.Provided)
	}
	e.strings(value.Values)
	e.u32(uint32(len(value.Fields)))
	for _, field := range value.Fields {
		e.string(field.PkgPath)
		e.string(field.Parent)
		e.string(field.Name)
		e.strings(field.Out)
	}
	e.strings(value.InputTypes)
}

type binarySummaryDecoder struct {
	r   *bytes.Reader
	err error
}

func (d *binarySummaryDecoder) u32() uint32 {
	if d.err != nil {
		return 0
	}
	var v uint32
	d.err = binary.Read(d.r, binary.LittleEndian, &v)
	return v
}

func (d *binarySummaryDecoder) string() string {
	n := d.u32()
	if d.err != nil {
		return ""
	}
	buf := make([]byte, n)
	_, d.err = d.r.Read(buf)
	return string(buf)
}

func (d *binarySummaryDecoder) bool() bool {
	if d.err != nil {
		return false
	}
	b, err := d.r.ReadByte()
	if err != nil {
		d.err = err
		return false
	}
	return b != 0
}

func (d *binarySummaryDecoder) strings() []string {
	n := d.u32()
	if d.err != nil {
		return nil
	}
	out := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		out = append(out, d.string())
	}
	return out
}

func (d *binarySummaryDecoder) providerSets() []providerSetSummary {
	n := d.u32()
	if d.err != nil {
		return nil
	}
	out := make([]providerSetSummary, 0, n)
	for i := uint32(0); i < n; i++ {
		out = append(out, d.providerSet())
	}
	return out
}

func (d *binarySummaryDecoder) providerSet() providerSetSummary {
	value := providerSetSummary{
		VarName: d.string(),
	}
	for n := d.u32(); n > 0; n-- {
		provider := providerSummary{
			PkgPath: d.string(),
			Name:    d.string(),
		}
		for m := d.u32(); m > 0; m-- {
			provider.Args = append(provider.Args, providerInputSummary{
				Type:      d.string(),
				FieldName: d.string(),
			})
		}
		provider.Out = d.strings()
		provider.Varargs = d.bool()
		provider.IsStruct = d.bool()
		provider.HasCleanup = d.bool()
		provider.HasErr = d.bool()
		value.Providers = append(value.Providers, provider)
	}
	for n := d.u32(); n > 0; n-- {
		value.Imports = append(value.Imports, providerSetRefSummary{
			PkgPath: d.string(),
			VarName: d.string(),
		})
	}
	for n := d.u32(); n > 0; n-- {
		value.Bindings = append(value.Bindings, ifaceBindingSummary{
			Iface:    d.string(),
			Provided: d.string(),
		})
	}
	value.Values = d.strings()
	for n := d.u32(); n > 0; n-- {
		value.Fields = append(value.Fields, fieldSummary{
			PkgPath: d.string(),
			Parent:  d.string(),
			Name:    d.string(),
			Out:     d.strings(),
		})
	}
	value.InputTypes = d.strings()
	return value
}
