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
	"time"
)

type timingLogger func(string, time.Duration)

type timingKey struct{}

// WithTiming enables timing output for wire operations using the provided
// callback.
func WithTiming(ctx context.Context, logf func(string, time.Duration)) context.Context {
	if logf == nil {
		return ctx
	}
	return context.WithValue(ctx, timingKey{}, timingLogger(logf))
}

func timing(ctx context.Context) timingLogger {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(timingKey{}); v != nil {
		if t, ok := v.(timingLogger); ok {
			return t
		}
	}
	return nil
}

func logTiming(ctx context.Context, label string, start time.Time) {
	if t := timing(ctx); t != nil {
		t(label, time.Since(start))
	}
}
