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

import "strings"

const ModeEnvVar = "WIRE_LOADER_MODE"

func ModeFromEnv(env []string) Mode {
	mode := ModeAuto
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name != ModeEnvVar {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case string(ModeCustom):
			mode = ModeCustom
		case string(ModeFallback):
			mode = ModeFallback
		case "", string(ModeAuto):
			mode = ModeAuto
		}
	}
	return mode
}
