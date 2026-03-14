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

package main

import (
	"context"
	"flag"
	"strconv"

	"github.com/goforj/wire/internal/wire"
)

type optionalBoolFlag struct {
	value bool
	set   bool
}

func (f *optionalBoolFlag) String() string {
	if f == nil {
		return ""
	}
	return strconv.FormatBool(f.value)
}

func (f *optionalBoolFlag) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	f.value = v
	f.set = true
	return nil
}

func (f *optionalBoolFlag) IsBoolFlag() bool {
	return true
}

func (f *optionalBoolFlag) apply(ctx context.Context) context.Context {
	if f == nil || !f.set {
		return ctx
	}
	return wire.WithIncremental(ctx, f.value)
}

func addIncrementalFlag(f *optionalBoolFlag, fs *flag.FlagSet) {
	fs.Var(f, "incremental", "enable the incremental engine (overrides "+wire.IncrementalEnvVar+")")
}
