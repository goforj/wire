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
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestWireGoGeneratePath(t *testing.T) {
	tests := []struct {
		name    string
		imports map[string]*packages.Package
		want    string
	}{
		{
			name: "google",
			imports: map[string]*packages.Package{
				"github.com/google/wire": {},
			},
			want: "github.com/google/wire",
		},
		{
			name: "goforj",
			imports: map[string]*packages.Package{
				"github.com/goforj/wire": {},
			},
			want: "github.com/goforj/wire",
		},
		{
			name: "default",
			want: "github.com/goforj/wire",
		},
	}
	for _, test := range tests {
		pkg := &packages.Package{Imports: test.imports}
		if got := wireGoGeneratePath(pkg); got != test.want {
			t.Fatalf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
}
