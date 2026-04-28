package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/goforj/wire/internal/wire"
)

func runGenerateCommand(ctx context.Context, wd string, env []string, patterns []string, opts *wire.GenerateOptions, timings bool) bool {
	totalStart := time.Now()
	genStart := time.Now()
	outs, errs := wire.Generate(ctx, wd, env, patterns, opts)
	logTiming(timings, "wire.Generate", genStart)
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("generate failed")
		return false
	}
	if len(outs) == 0 {
		logTiming(timings, "total", totalStart)
		return true
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
			continue
		}
		if wrote, err := out.CommitWithStatus(); err == nil {
			if wrote {
				logSuccessf("%s: wrote %s (%s)", out.PkgPath, out.OutputPath, formatDuration(time.Since(totalStart)))
			} else {
				logSuccessf("%s: unchanged %s (%s)", out.PkgPath, out.OutputPath, formatDuration(time.Since(totalStart)))
			}
		} else {
			log.Printf("%s: failed to write %s: %v\n", out.PkgPath, out.OutputPath, err)
			success = false
		}
	}
	if !success {
		log.Println("at least one generate failure")
		return false
	}
	logTiming(timings, "writes", writeStart)
	logTiming(timings, "total", totalStart)
	return true
}

func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
}
