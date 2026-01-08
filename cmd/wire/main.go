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

// Wire is a compile-time dependency injection tool.
//
// For an overview, see https://github.com/goforj/wire/blob/master/README.md
package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"time"

	"github.com/goforj/wire/internal/wire"
	"github.com/google/subcommands"
)

// main wires up subcommands and executes the selected command.
func main() {
	subcommands.Register(subcommands.CommandsCommand(), "")
	subcommands.Register(subcommands.FlagsCommand(), "")
	subcommands.Register(subcommands.HelpCommand(), "")
	subcommands.Register(&checkCmd{}, "")
	subcommands.Register(&cacheCmd{}, "")
	subcommands.Register(&diffCmd{}, "")
	subcommands.Register(&genCmd{}, "")
	subcommands.Register(&showCmd{}, "")
	flag.Parse()

	// Initialize the default logger to log to stderr.
	log.SetFlags(0)
	log.SetPrefix("wire: ")
	log.SetOutput(os.Stderr)

	// TODO(rvangent): Use subcommands's VisitCommands instead of hardcoded map,
	// once there is a release that contains it:
	// allCmds := map[string]bool{}
	// subcommands.DefaultCommander.VisitCommands(func(_ *subcommands.CommandGroup, cmd subcommands.Command) { allCmds[cmd.Name()] = true })
	allCmds := map[string]bool{
		"commands": true, // builtin
		"help":     true, // builtin
		"flags":    true, // builtin
		"check":    true,
		"cache":    true,
		"diff":     true,
		"gen":      true,
		"serve":    true,
		"show":     true,
	}
	// Default to running the "gen" command.
	if args := flag.Args(); len(args) == 0 || !allCmds[args[0]] {
		genCmd := &genCmd{}
		os.Exit(int(genCmd.Execute(context.Background(), flag.CommandLine)))
	}
	os.Exit(int(subcommands.Execute(context.Background())))
}

// installStackDumper registers signal handlers to dump goroutine stacks.
// packages returns the slice of packages to run wire over based on f.
// It defaults to ".".
// packages returns the packages selected by command-line args.
func packages(f *flag.FlagSet) []string {
	pkgs := f.Args()
	if len(pkgs) == 0 {
		pkgs = []string{"."}
	}
	return pkgs
}

type profileFlags struct {
	cpuProfile   string
	memProfile   string
	traceProfile string
	timings      bool
}

// addFlags registers profiling flags on the provided FlagSet.
func (pf *profileFlags) addFlags(f *flag.FlagSet) {
	f.StringVar(&pf.cpuProfile, "cpuprofile", "", "write CPU profile to file")
	f.StringVar(&pf.memProfile, "memprofile", "", "write memory profile to file")
	f.StringVar(&pf.traceProfile, "trace", "", "write execution trace to file")
	f.BoolVar(&pf.timings, "timings", false, "log timing information for major steps")
}

// start enables configured profiles and returns a stop function.
func (pf *profileFlags) start() (func(), error) {
	var cpuFile *os.File
	var traceFile *os.File

	if pf.cpuProfile != "" {
		f, err := os.Create(pf.cpuProfile)
		if err != nil {
			return nil, fmt.Errorf("failed to create CPU profile %q: %v", pf.cpuProfile, err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to start CPU profile: %v", err)
		}
		cpuFile = f
	}

	if pf.traceProfile != "" {
		f, err := os.Create(pf.traceProfile)
		if err != nil {
			if cpuFile != nil {
				pprof.StopCPUProfile()
				cpuFile.Close()
			}
			return nil, fmt.Errorf("failed to create trace profile %q: %v", pf.traceProfile, err)
		}
		if err := trace.Start(f); err != nil {
			f.Close()
			if cpuFile != nil {
				pprof.StopCPUProfile()
				cpuFile.Close()
			}
			return nil, fmt.Errorf("failed to start trace: %v", err)
		}
		traceFile = f
	}

	stop := func() {
		if traceFile != nil {
			trace.Stop()
			traceFile.Close()
		}
		if cpuFile != nil {
			pprof.StopCPUProfile()
			cpuFile.Close()
		}
		if pf.memProfile != "" {
			f, err := os.Create(pf.memProfile)
			if err != nil {
				log.Printf("failed to create memory profile %q: %v", pf.memProfile, err)
				return
			}
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Printf("failed to write memory profile: %v", err)
			}
			f.Close()
		}
	}
	return stop, nil
}

// logTiming writes a timing log entry when enabled.
func logTiming(enabled bool, label string, start time.Time) {
	if enabled {
		log.Printf("timing: %s=%s", label, time.Since(start))
	}
}

// withTiming attaches a timing logger to the context when enabled.
func withTiming(ctx context.Context, enabled bool) context.Context {
	if !enabled {
		return ctx
	}
	return wire.WithTiming(ctx, func(label string, dur time.Duration) {
		log.Printf("timing: %s=%s", label, dur)
	})
}

// newGenerateOptions returns an initialized wire.GenerateOptions, possibly
// with the Header option set.
// newGenerateOptions builds GenerateOptions, loading the header if set.
func newGenerateOptions(headerFile string) (*wire.GenerateOptions, error) {
	opts := new(wire.GenerateOptions)
	if headerFile != "" {
		var err error
		opts.Header, err = ioutil.ReadFile(headerFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read header file %q: %v", headerFile, err)
		}
	}
	return opts, nil
}

// logErrors logs each error with consistent formatting.
func logErrors(errs []error) {
	for _, err := range errs {
		log.Println(strings.Replace(err.Error(), "\n", "\n\t", -1))
	}
}
