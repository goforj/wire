// Copyright 2018 The Wire Authors
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
	"fmt"
	"log"

	"github.com/goforj/wire/internal/wire"
	"github.com/google/subcommands"
)

type cacheCmd struct {
	clear bool
}

func (*cacheCmd) Name() string { return "cache" }
func (*cacheCmd) Synopsis() string {
	return "inspect or clear the wire cache"
}
func (*cacheCmd) Usage() string {
	return `cache [-clear]

  By default, prints the cache directory. With -clear, removes all cache files.
`
}
func (cmd *cacheCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&cmd.clear, "clear", false, "remove all cached data")
}
func (cmd *cacheCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if cmd.clear {
		if err := wire.ClearCache(); err != nil {
			log.Printf("failed to clear cache: %v\n", err)
			return subcommands.ExitFailure
		}
		log.Printf("cleared cache at %s\n", wire.CacheDir())
		return subcommands.ExitSuccess
	}
	fmt.Println(wire.CacheDir())
	return subcommands.ExitSuccess
}
