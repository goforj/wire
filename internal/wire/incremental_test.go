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
)

func TestIncrementalEnabledDefaultOff(t *testing.T) {
	if IncrementalEnabled(context.Background(), nil) {
		t.Fatal("IncrementalEnabled should default to false")
	}
}

func TestIncrementalEnabledFromEnv(t *testing.T) {
	env := []string{
		"FOO=bar",
		IncrementalEnvVar + "=true",
	}
	if !IncrementalEnabled(context.Background(), env) {
		t.Fatal("IncrementalEnabled should read the environment variable")
	}
}

func TestIncrementalEnabledUsesLastEnvValue(t *testing.T) {
	env := []string{
		IncrementalEnvVar + "=false",
		IncrementalEnvVar + "=true",
	}
	if !IncrementalEnabled(context.Background(), env) {
		t.Fatal("IncrementalEnabled should use the last matching env value")
	}
}

func TestIncrementalEnabledContextOverridesEnv(t *testing.T) {
	env := []string{
		IncrementalEnvVar + "=false",
	}
	ctx := WithIncremental(context.Background(), true)
	if !IncrementalEnabled(ctx, env) {
		t.Fatal("context override should take precedence over env")
	}
}

func TestIncrementalEnabledInvalidEnvFallsBackFalse(t *testing.T) {
	env := []string{
		IncrementalEnvVar + "=maybe",
	}
	if IncrementalEnabled(context.Background(), env) {
		t.Fatal("invalid env value should not enable incremental mode")
	}
}
