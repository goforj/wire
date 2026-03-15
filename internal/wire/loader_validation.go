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

	"github.com/goforj/wire/internal/loader"
)

func loaderValidationMode(ctx context.Context, wd string, env []string) bool {
	return effectiveLoaderMode(ctx, wd, env) != loader.ModeFallback
}

func effectiveLoaderMode(ctx context.Context, wd string, env []string) loader.Mode {
	mode := loader.ModeFromEnv(env)
	if mode != loader.ModeAuto {
		return mode
	}
	return loader.ModeAuto
}
