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
	"testing"
	"time"
)

func TestWithTimingNil(t *testing.T) {
	ctx := context.Background()
	if got := WithTiming(ctx, nil); got != ctx {
		t.Fatal("expected WithTiming to return original context on nil logger")
	}
	if timing(nil) != nil {
		t.Fatal("expected nil timing for nil context")
	}
}

func TestWithTimingAndLog(t *testing.T) {
	ctx := context.Background()
	called := false
	ctx = WithTiming(ctx, func(label string, d time.Duration) {
		if label != "test" {
			t.Fatalf("unexpected label: %q", label)
		}
		called = true
	})

	if timing(context.Background()) != nil {
		t.Fatal("expected no timing logger on plain context")
	}

	logTiming(ctx, "test", time.Now())
	if !called {
		t.Fatal("expected timing logger to be called")
	}
}
