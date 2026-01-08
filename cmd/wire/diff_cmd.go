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
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/goforj/wire/internal/wire"
	"github.com/google/subcommands"
	"github.com/pmezard/go-difflib/difflib"
)

type diffCmd struct {
	headerFile string
	tags       string
	profile    profileFlags
}

func (*diffCmd) Name() string { return "diff" }
func (*diffCmd) Synopsis() string {
	return "output a diff between existing wire_gen.go files and what gen would generate"
}
func (*diffCmd) Usage() string {
	return `diff [packages]

  Given one or more packages, diff generates the content for their wire_gen.go
  files and outputs the diff against the existing files.

  If no packages are listed, it defaults to ".".

  Similar to the diff command, it returns 0 if no diff, 1 if different, 2
  plus an error if trouble.
`
}
func (cmd *diffCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.headerFile, "header_file", "", "path to file to insert as a header in wire_gen.go")
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
	cmd.profile.addFlags(f)
}
func (cmd *diffCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	const (
		errReturn  = subcommands.ExitStatus(2)
		diffReturn = subcommands.ExitStatus(1)
	)
	stop, err := cmd.profile.start()
	if err != nil {
		log.Println(err)
		return errReturn
	}
	defer stop()
	totalStart := time.Now()
	ctx = withTiming(ctx, cmd.profile.timings)

	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return errReturn
	}
	opts, err := newGenerateOptions(cmd.headerFile)
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}

	opts.Tags = cmd.tags

	genStart := time.Now()
	outs, errs := wire.Generate(ctx, wd, os.Environ(), packages(f), opts)
	logTiming(cmd.profile.timings, "wire.Generate", genStart)
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("generate failed")
		return errReturn
	}
	if len(outs) == 0 {
		logTiming(cmd.profile.timings, "total", totalStart)
		return subcommands.ExitSuccess
	}
	success := true
	hadDiff := false
	diffStart := time.Now()
	for _, out := range outs {
		if len(out.Errs) > 0 {
			logErrors(out.Errs)
			log.Printf("%s: generate failed\n", out.PkgPath)
			success = false
		}
		if len(out.Content) == 0 {
			// No Wire output. Maybe errors, maybe no Wire directives.
			continue
		}
		// Assumes the current file is empty if we can't read it.
		cur, _ := ioutil.ReadFile(out.OutputPath)
		if diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A: difflib.SplitLines(string(cur)),
			B: difflib.SplitLines(string(out.Content)),
		}); err == nil {
			if diff != "" {
				// Print the actual diff to stdout, not stderr.
				fmt.Printf("%s: diff from %s:\n%s\n", out.PkgPath, out.OutputPath, diff)
				hadDiff = true
			}
		} else {
			log.Printf("%s: failed to diff %s: %v\n", out.PkgPath, out.OutputPath, err)
			success = false
		}
	}
	if !success {
		log.Println("at least one generate failure")
		return errReturn
	}
	logTiming(cmd.profile.timings, "diffs", diffStart)
	logTiming(cmd.profile.timings, "total", totalStart)
	if hadDiff {
		return diffReturn
	}
	return subcommands.ExitSuccess
}
