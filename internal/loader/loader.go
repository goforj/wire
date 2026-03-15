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
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/packages"
)

type Mode string

const (
	ModeAuto     Mode = "auto"
	ModeCustom   Mode = "custom"
	ModeFallback Mode = "fallback"
)

type FallbackReason string

const (
	FallbackReasonNone                 FallbackReason = ""
	FallbackReasonForcedFallback       FallbackReason = "forced_fallback"
	FallbackReasonCustomNotImplemented FallbackReason = "custom_not_implemented"
	FallbackReasonCustomUnsupported    FallbackReason = "custom_unsupported"
)

type LocalPackageFingerprint struct {
	PkgPath     string
	ContentHash string
	ShapeHash   string
	Files       []string
}

type DiscoverySnapshot struct {
	meta map[string]*packageMeta
}

type TouchedValidationRequest struct {
	WD      string
	Env     []string
	Tags    string
	Touched []string
	Local   []LocalPackageFingerprint
	Mode    Mode
}

type TouchedValidationResult struct {
	Packages       []*packages.Package
	Backend        Mode
	FallbackReason FallbackReason
	FallbackDetail string
}

type RootLoadRequest struct {
	WD       string
	Env      []string
	Tags     string
	Patterns []string
	NeedDeps bool
	Mode     Mode
	Fset     *token.FileSet
}

type RootLoadResult struct {
	Packages       []*packages.Package
	Backend        Mode
	FallbackReason FallbackReason
	FallbackDetail string
	Discovery      *DiscoverySnapshot
}

type PackageLoadRequest struct {
	WD         string
	Env        []string
	Tags       string
	Patterns   []string
	Mode       packages.LoadMode
	LoaderMode Mode
	Fset       *token.FileSet
	ParseFile  ParseFileFunc
}

type PackageLoadResult struct {
	Packages       []*packages.Package
	Backend        Mode
	FallbackReason FallbackReason
	FallbackDetail string
}

type ParseFileFunc func(*token.FileSet, string, []byte) (*ast.File, error)

type LazyLoadRequest struct {
	WD         string
	Env        []string
	Tags       string
	Package    string
	Mode       packages.LoadMode
	LoaderMode Mode
	Fset       *token.FileSet
	ParseFile  ParseFileFunc
	Discovery  *DiscoverySnapshot
}

type LazyLoadResult struct {
	Packages       []*packages.Package
	Backend        Mode
	FallbackReason FallbackReason
	FallbackDetail string
}

type Loader interface {
	LoadPackages(context.Context, PackageLoadRequest) (*PackageLoadResult, error)
	LoadRootGraph(context.Context, RootLoadRequest) (*RootLoadResult, error)
	LoadTypedPackageGraph(context.Context, LazyLoadRequest) (*LazyLoadResult, error)
	ValidateTouchedPackages(context.Context, TouchedValidationRequest) (*TouchedValidationResult, error)
}

func New() Loader {
	return defaultLoader{}
}
