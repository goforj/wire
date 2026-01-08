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
	"log"
	"os"
	"time"

	"github.com/goforj/wire/internal/wire"
	"github.com/google/subcommands"
)

type checkCmd struct {
	tags    string
	profile profileFlags
}

func (*checkCmd) Name() string { return "check" }
func (*checkCmd) Synopsis() string {
	return "print any Wire errors found"
}
func (*checkCmd) Usage() string {
	return `check [-tags tag,list] [packages]

  Given one or more packages, check prints any type-checking or Wire errors
  found with top-level variable provider sets or injector functions.

  If no packages are listed, it defaults to ".".
`
}
func (cmd *checkCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
	cmd.profile.addFlags(f)
}
func (cmd *checkCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	stop, err := cmd.profile.start()
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}
	defer stop()
	totalStart := time.Now()
	ctx = withTiming(ctx, cmd.profile.timings)

	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return subcommands.ExitFailure
	}
	loadStart := time.Now()
	_, errs := wire.Load(ctx, wd, os.Environ(), cmd.tags, packages(f))
	logTiming(cmd.profile.timings, "wire.Load", loadStart)
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("error loading packages")
		return subcommands.ExitFailure
	}
	logTiming(cmd.profile.timings, "total", totalStart)
	return subcommands.ExitSuccess
}
