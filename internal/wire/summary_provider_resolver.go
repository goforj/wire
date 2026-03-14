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
	"context"
	"fmt"
	"go/token"
	"go/types"
	"time"

	"golang.org/x/tools/go/types/typeutil"
)

type summaryProviderResolver struct {
	ctx           context.Context
	fset          *token.FileSet
	summaries     map[string]*packageSummary
	importPackage func(string) (*types.Package, error)
	cache         map[providerSetRefSummary]*ProviderSet
	resolving     map[providerSetRefSummary]struct{}
	supported     map[string]bool
}

func newSummaryProviderResolver(ctx context.Context, summaries map[string]*packageSummary, importPackage func(string) (*types.Package, error)) *summaryProviderResolver {
	if len(summaries) == 0 || importPackage == nil {
		return nil
	}
	r := &summaryProviderResolver{
		ctx:           ctx,
		fset:          token.NewFileSet(),
		summaries:     make(map[string]*packageSummary, len(summaries)),
		importPackage: importPackage,
		cache:         make(map[providerSetRefSummary]*ProviderSet),
		resolving:     make(map[providerSetRefSummary]struct{}),
		supported:     make(map[string]bool, len(summaries)),
	}
	for pkgPath, summary := range summaries {
		if summary == nil {
			continue
		}
		r.summaries[pkgPath] = summary
	}
	for pkgPath := range r.summaries {
		r.supported[pkgPath] = r.packageSupported(pkgPath, make(map[string]struct{}))
	}
	return r
}

func filterSupportedPackageSummaries(summaries map[string]*packageSummary) map[string]*packageSummary {
	if len(summaries) == 0 {
		return nil
	}
	resolver := &summaryProviderResolver{
		summaries: summaries,
		supported: make(map[string]bool, len(summaries)),
	}
	out := make(map[string]*packageSummary)
	for pkgPath, summary := range summaries {
		if summary == nil {
			continue
		}
		if resolver.packageSupported(pkgPath, make(map[string]struct{})) {
			out[pkgPath] = summary
		}
	}
	return out
}

func (r *summaryProviderResolver) Resolve(pkgPath string, varName string) (*ProviderSet, bool, []error) {
	if r == nil || !r.supported[pkgPath] {
		return nil, false, nil
	}
	start := time.Now()
	set, err := r.resolve(providerSetRefSummary{PkgPath: pkgPath, VarName: varName})
	logTiming(r.ctx, "incremental.local_fastpath.summary_resolve", start)
	if err != nil {
		return nil, true, []error{err}
	}
	return set, true, nil
}

func (r *summaryProviderResolver) resolve(ref providerSetRefSummary) (*ProviderSet, error) {
	if set := r.cache[ref]; set != nil {
		return set, nil
	}
	if _, ok := r.resolving[ref]; ok {
		return nil, fmt.Errorf("summary provider set cycle for %s.%s", ref.PkgPath, ref.VarName)
	}
	summary := r.summaries[ref.PkgPath]
	if summary == nil {
		return nil, fmt.Errorf("missing package summary for %s", ref.PkgPath)
	}
	setSummary, ok := r.findProviderSet(summary, ref.VarName)
	if !ok {
		return nil, fmt.Errorf("missing provider set summary for %s.%s", ref.PkgPath, ref.VarName)
	}
	r.resolving[ref] = struct{}{}
	defer delete(r.resolving, ref)

	pkg, err := r.importPackage(ref.PkgPath)
	if err != nil {
		return nil, err
	}
	set := &ProviderSet{
		PkgPath: ref.PkgPath,
		VarName: ref.VarName,
	}
	for _, provider := range setSummary.Providers {
		resolved, err := r.resolveProvider(pkg, provider)
		if err != nil {
			return nil, err
		}
		set.Providers = append(set.Providers, resolved)
	}
	for _, imported := range setSummary.Imports {
		child, err := r.resolve(imported)
		if err != nil {
			return nil, err
		}
		set.Imports = append(set.Imports, child)
	}
	hasher := typeutil.MakeHasher()
	providerMap, srcMap, errs := buildProviderMap(r.fset, hasher, set)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	if errs := verifyAcyclic(providerMap, hasher); len(errs) > 0 {
		return nil, errs[0]
	}
	set.providerMap = providerMap
	set.srcMap = srcMap
	r.cache[ref] = set
	return set, nil
}

func (r *summaryProviderResolver) resolveProvider(pkg *types.Package, summary providerSummary) (*Provider, error) {
	if summary.IsStruct || len(summary.Out) == 0 {
		return nil, fmt.Errorf("unsupported summary provider %s.%s", summary.PkgPath, summary.Name)
	}
	if pkg == nil || pkg.Path() != summary.PkgPath {
		var err error
		pkg, err = r.importPackage(summary.PkgPath)
		if err != nil {
			return nil, err
		}
	}
	obj := pkg.Scope().Lookup(summary.Name)
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil, fmt.Errorf("summary provider %s.%s missing function", summary.PkgPath, summary.Name)
	}
	provider, errs := processFuncProvider(r.fset, fn)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return provider, nil
}

func (r *summaryProviderResolver) findProviderSet(summary *packageSummary, varName string) (providerSetSummary, bool) {
	if summary == nil {
		return providerSetSummary{}, false
	}
	for _, set := range summary.ProviderSets {
		if set.VarName == varName {
			return set, true
		}
	}
	return providerSetSummary{}, false
}

func (r *summaryProviderResolver) packageSupported(pkgPath string, visiting map[string]struct{}) bool {
	if ok, seen := r.supported[pkgPath]; seen {
		return ok
	}
	if _, seen := visiting[pkgPath]; seen {
		return false
	}
	summary := r.summaries[pkgPath]
	if summary == nil {
		return false
	}
	visiting[pkgPath] = struct{}{}
	defer delete(visiting, pkgPath)
	for _, set := range summary.ProviderSets {
		if !providerSetSummarySupported(set) {
			return false
		}
		for _, imported := range set.Imports {
			if _, ok := r.summaries[imported.PkgPath]; !ok {
				return false
			}
			if !r.packageSupported(imported.PkgPath, visiting) {
				return false
			}
		}
	}
	return true
}

func providerSetSummarySupported(summary providerSetSummary) bool {
	if len(summary.Bindings) > 0 || len(summary.Values) > 0 || len(summary.Fields) > 0 || len(summary.InputTypes) > 0 {
		return false
	}
	for _, provider := range summary.Providers {
		if provider.IsStruct {
			return false
		}
	}
	return true
}
