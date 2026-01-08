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
	"os/signal"
	"syscall"
	"time"

	"github.com/goforj/wire/internal/wire"
	"github.com/google/subcommands"
)

type serveCmd struct {
	headerFile     string
	prefixFileName string
	tags           string
	interval       time.Duration
	timings        bool
}

func (*serveCmd) Name() string { return "serve" }
func (*serveCmd) Synopsis() string {
	return "watch for changes and regenerate wire output"
}
func (*serveCmd) Usage() string {
	return `serve [packages]

  Serve watches for Go file changes and regenerates wire output when changes
  are detected. It exits on error or interrupt.
`
}
func (cmd *serveCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.headerFile, "header_file", "", "path to file to insert as a header in wire_gen.go")
	f.StringVar(&cmd.prefixFileName, "output_file_prefix", "", "string to prepend to output file names.")
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
	f.DurationVar(&cmd.interval, "interval", 250*time.Millisecond, "poll interval for filesystem changes")
	f.BoolVar(&cmd.timings, "timings", false, "log timing information for major steps")
}
func (cmd *serveCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
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

	ctx = withTiming(ctx, cmd.timings)
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := wire.Serve(ctx, wd, os.Environ(), packages(f), opts, cmd.interval); err != nil && err != context.Canceled {
		log.Printf("serve failed: %v\n", err)
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}
