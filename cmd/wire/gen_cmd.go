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

type genCmd struct {
	headerFile     string
	prefixFileName string
	tags           string
	profile        profileFlags
}

// Name returns the subcommand name.
func (*genCmd) Name() string { return "gen" }

// Synopsis returns a short summary of the subcommand.
func (*genCmd) Synopsis() string {
	return "generate the wire_gen.go file for each package"
}

// Usage returns the help text for the subcommand.
func (*genCmd) Usage() string {
	return `gen [packages]

  Given one or more packages, gen creates the wire_gen.go file for each.

  If no packages are listed, it defaults to ".".
`
}

// SetFlags registers flags for the subcommand.
func (cmd *genCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.headerFile, "header_file", "", "path to file to insert as a header in wire_gen.go")
	f.StringVar(&cmd.prefixFileName, "output_file_prefix", "", "string to prepend to output file names.")
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
	cmd.profile.addFlags(f)
}

// Execute runs the subcommand.
func (cmd *genCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
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
	opts, err := newGenerateOptions(cmd.headerFile)
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}

	opts.PrefixOutputFile = cmd.prefixFileName
	opts.Tags = cmd.tags

	genStart := time.Now()
	outs, errs := wire.Generate(ctx, wd, os.Environ(), packages(f), opts)
	logTiming(cmd.profile.timings, "wire.Generate", genStart)
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("generate failed")
		return subcommands.ExitFailure
	}
	if len(outs) == 0 {
		logTiming(cmd.profile.timings, "total", totalStart)
		return subcommands.ExitSuccess
	}
	success := true
	writeStart := time.Now()
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
		if err := out.Commit(); err == nil {
			log.Printf("%s: wrote %s\n", out.PkgPath, out.OutputPath)
		} else {
			log.Printf("%s: failed to write %s: %v\n", out.PkgPath, out.OutputPath, err)
			success = false
		}
	}
	if !success {
		log.Println("at least one generate failure")
		return subcommands.ExitFailure
	}
	logTiming(cmd.profile.timings, "writes", writeStart)
	logTiming(cmd.profile.timings, "total", totalStart)
	return subcommands.ExitSuccess
}
