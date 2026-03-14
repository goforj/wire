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
	"strconv"
	"strings"
)

const IncrementalEnvVar = "WIRE_INCREMENTAL"

type incrementalKey struct{}

// WithIncremental overrides incremental-mode resolution for the provided
// context. This takes precedence over the environment variable.
func WithIncremental(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, incrementalKey{}, enabled)
}

// IncrementalEnabled reports whether incremental mode is enabled for the
// current operation. A context override takes precedence over env.
func IncrementalEnabled(ctx context.Context, env []string) bool {
	if ctx != nil {
		if v := ctx.Value(incrementalKey{}); v != nil {
			if enabled, ok := v.(bool); ok {
				return enabled
			}
		}
	}
	raw, ok := lookupEnv(env, IncrementalEnvVar)
	if !ok {
		return false
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return enabled
}

func lookupEnv(env []string, key string) (string, bool) {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix), true
		}
	}
	return "", false
}
