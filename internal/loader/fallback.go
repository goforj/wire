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

package loader

import (
	"context"
	"errors"
	"go/token"

	"golang.org/x/tools/go/packages"
)

type defaultLoader struct{}

func fallbackReasonDetail(mode Mode, detail string) (FallbackReason, string) {
	switch mode {
	case ModeFallback:
		return FallbackReasonForcedFallback, ""
	default:
		return FallbackReasonCustomUnsupported, detail
	}
}

func (defaultLoader) LoadPackages(ctx context.Context, req PackageLoadRequest) (*PackageLoadResult, error) {
	var unsupported unsupportedError
	if req.LoaderMode != ModeFallback {
		result, err := loadPackagesCustom(ctx, req)
		if err == nil {
			return result, nil
		}
		if !errors.As(err, &unsupported) {
			return nil, err
		}
	}
	result := &PackageLoadResult{
		Backend: ModeFallback,
	}
	result.FallbackReason, result.FallbackDetail = fallbackReasonDetail(req.LoaderMode, unsupported.reason)
	cfg := &packages.Config{
		Context:    ctx,
		Mode:       req.Mode,
		Dir:        req.WD,
		Env:        req.Env,
		BuildFlags: []string{"-tags=wireinject"},
		Fset:       req.Fset,
	}
	if cfg.Fset == nil {
		cfg.Fset = token.NewFileSet()
	}
	if req.ParseFile != nil {
		cfg.ParseFile = req.ParseFile
	}
	if req.Tags != "" {
		cfg.BuildFlags[0] += " " + req.Tags
	}
	escaped := make([]string, len(req.Patterns))
	for i := range req.Patterns {
		escaped[i] = "pattern=" + req.Patterns[i]
	}
	pkgs, err := packages.Load(cfg, escaped...)
	if err != nil {
		return nil, err
	}
	result.Packages = pkgs
	return result, nil
}

func (defaultLoader) LoadRootGraph(ctx context.Context, req RootLoadRequest) (*RootLoadResult, error) {
	var unsupported unsupportedError
	if req.Mode != ModeFallback {
		result, err := loadRootGraphCustom(ctx, req)
		if err == nil {
			return result, nil
		}
		if !errors.As(err, &unsupported) {
			return nil, err
		}
	}
	result := &RootLoadResult{
		Backend: ModeFallback,
	}
	result.FallbackReason, result.FallbackDetail = fallbackReasonDetail(req.Mode, unsupported.reason)
	cfg := &packages.Config{
		Context:    ctx,
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports,
		Dir:        req.WD,
		Env:        req.Env,
		BuildFlags: []string{"-tags=wireinject"},
		Fset:       req.Fset,
	}
	if req.NeedDeps {
		cfg.Mode |= packages.NeedDeps
	}
	if req.Fset == nil {
		cfg.Fset = token.NewFileSet()
	}
	if req.Tags != "" {
		cfg.BuildFlags[0] += " " + req.Tags
	}
	escaped := make([]string, len(req.Patterns))
	for i := range req.Patterns {
		escaped[i] = "pattern=" + req.Patterns[i]
	}
	pkgs, err := packages.Load(cfg, escaped...)
	if err != nil {
		return nil, err
	}
	result.Packages = pkgs
	return result, nil
}

func (defaultLoader) LoadTypedPackageGraph(ctx context.Context, req LazyLoadRequest) (*LazyLoadResult, error) {
	var unsupported unsupportedError
	if req.LoaderMode != ModeFallback {
		result, err := loadTypedPackageGraphCustom(ctx, req)
		if err == nil {
			return result, nil
		}
		if !errors.As(err, &unsupported) {
			return nil, err
		}
	}
	result := &LazyLoadResult{
		Backend: ModeFallback,
	}
	result.FallbackReason, result.FallbackDetail = fallbackReasonDetail(req.LoaderMode, unsupported.reason)
	cfg := &packages.Config{
		Context:    ctx,
		Mode:       req.Mode,
		Dir:        req.WD,
		Env:        req.Env,
		BuildFlags: []string{"-tags=wireinject"},
		Fset:       req.Fset,
	}
	if cfg.Fset == nil {
		cfg.Fset = token.NewFileSet()
	}
	if req.ParseFile != nil {
		cfg.ParseFile = req.ParseFile
	}
	if req.Tags != "" {
		cfg.BuildFlags[0] += " " + req.Tags
	}
	pkgs, err := packages.Load(cfg, "pattern="+req.Package)
	if err != nil {
		return nil, err
	}
	result.Packages = pkgs
	return result, nil
}

func (defaultLoader) ValidateTouchedPackages(ctx context.Context, req TouchedValidationRequest) (*TouchedValidationResult, error) {
	var unsupported unsupportedError
	if req.Mode != ModeFallback {
		result, err := validateTouchedPackagesCustom(ctx, req)
		if err == nil {
			return result, nil
		}
		if !errors.As(err, &unsupported) {
			return nil, err
		}
	}
	return validateTouchedPackagesFallback(ctx, req, unsupported.reason)
}

func validateTouchedPackagesFallback(ctx context.Context, req TouchedValidationRequest, detail string) (*TouchedValidationResult, error) {
	result := &TouchedValidationResult{
		Backend: ModeFallback,
	}
	result.FallbackReason, result.FallbackDetail = fallbackReasonDetail(req.Mode, detail)
	if len(req.Touched) == 0 {
		return result, nil
	}
	cfg := &packages.Config{
		Context:    ctx,
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedExportsFile | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesSizes,
		Dir:        req.WD,
		Env:        req.Env,
		BuildFlags: []string{"-tags=wireinject"},
		Fset:       token.NewFileSet(),
	}
	if req.Tags != "" {
		cfg.BuildFlags[0] += " " + req.Tags
	}
	pkgs, err := packages.Load(cfg, req.Touched...)
	if err != nil {
		return nil, err
	}
	result.Packages = pkgs
	return result, nil
}
