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

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/goforj/wire/internal/wire"
	"github.com/google/subcommands"
)

// watchCmd implements the wire watch subcommand.
type watchCmd struct {
	headerFile     string
	prefixFileName string
	tags           string
	profile        profileFlags
	pollInterval   time.Duration
	rescanInterval time.Duration
}

// Name returns the subcommand name.
func (*watchCmd) Name() string { return "watch" }

// Synopsis returns a short summary of the subcommand.
func (*watchCmd) Synopsis() string {
	return "watch Go files and re-run wire gen on changes"
}

// Usage returns the help text for the subcommand.
func (*watchCmd) Usage() string {
	return `watch [packages]

  Given one or more packages, watch re-runs wire gen when Go files change.
  If no packages are listed, it defaults to ".".
`
}

// SetFlags registers flags for the subcommand.
func (cmd *watchCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.headerFile, "header_file", "", "path to file to insert as a header in wire_gen.go")
	f.StringVar(&cmd.prefixFileName, "output_file_prefix", "", "string to prepend to output file names.")
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
	f.DurationVar(&cmd.pollInterval, "poll_interval", 250*time.Millisecond, "interval between file stat checks")
	f.DurationVar(&cmd.rescanInterval, "rescan_interval", 2*time.Second, "interval to rescan for new or removed Go files")
	cmd.profile.addFlags(f)
}

// Execute runs the subcommand.
func (cmd *watchCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	stop, err := cmd.profile.start()
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}
	defer stop()
	ctx = withTiming(ctx, cmd.profile.timings)

	if cmd.pollInterval <= 0 {
		log.Println("poll_interval must be greater than zero")
		return subcommands.ExitFailure
	}
	if cmd.rescanInterval <= 0 {
		log.Println("rescan_interval must be greater than zero")
		return subcommands.ExitFailure
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory:", err)
		return subcommands.ExitFailure
	}
	opts, err := newGenerateOptions(cmd.headerFile)
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}
	opts.PrefixOutputFile = cmd.prefixFileName
	opts.Tags = cmd.tags

	env := os.Environ()
	runGenerate := func() {
		totalStart := time.Now()
		genStart := time.Now()
		outs, errs := wire.Generate(ctx, wd, env, packages(f), opts)
		logTiming(cmd.profile.timings, "wire.Generate", genStart)
		if len(errs) > 0 {
			logErrors(errs)
			log.Println("generate failed")
			return
		}
		if len(outs) == 0 {
			logTiming(cmd.profile.timings, "total", totalStart)
			return
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
			if err := out.Commit(); err == nil {
				log.Printf("%s: wrote %s (%s)\n", out.PkgPath, out.OutputPath, formatDuration(time.Since(totalStart)))
			} else {
				log.Printf("%s: failed to write %s: %v\n", out.PkgPath, out.OutputPath, err)
				success = false
			}
		}
		if !success {
			log.Println("at least one generate failure")
			return
		}
		logTiming(cmd.profile.timings, "writes", writeStart)
		logTiming(cmd.profile.timings, "total", totalStart)
	}

	root, err := moduleRoot(wd, env)
	if err != nil {
		log.Printf("watch: failed to resolve module root, using %s: %v", wd, err)
		root = wd
	}

	runGenerate()
	if err := watchWithFSNotify(root, runGenerate); err == nil {
		return subcommands.ExitSuccess
	} else {
		log.Printf("watch: fsnotify unavailable, falling back to polling: %v", err)
	}

	state, err := scanGoFiles(root)
	if err != nil {
		log.Printf("initial scan failed: %v", err)
	}
	state, _ = scanGoFiles(root)

	pollTicker := time.NewTicker(cmd.pollInterval)
	rescanTicker := time.NewTicker(cmd.rescanInterval)
	defer pollTicker.Stop()
	defer rescanTicker.Stop()

	for {
		select {
		case <-pollTicker.C:
			if changed := updateFileState(state); len(changed) > 0 {
				log.Printf("watch: changes detected (%s), re-running", formatChangedFiles(changed, root))
				runGenerate()
				state, _ = scanGoFiles(root)
			}
		case <-rescanTicker.C:
			newState, err := scanGoFiles(root)
			if err != nil {
				log.Printf("rescan failed: %v", err)
				continue
			}
			if changed := diffFileState(state, newState); len(changed) > 0 {
				log.Printf("watch: file set changed (%s), re-running", formatChangedFiles(changed, root))
				state = newState
				runGenerate()
				state, _ = scanGoFiles(root)
			} else {
				state = newState
			}
		}
	}
}

// fileState stores file metadata for polling-based change detection.
type fileState struct {
	modTime time.Time
	size    int64
}

// scanGoFiles recursively collects Go file metadata under root.
func scanGoFiles(root string) (map[string]fileState, error) {
	state := make(map[string]fileState)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if strings.HasSuffix(d.Name(), "wire_gen.go") {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		state[path] = fileState{
			modTime: info.ModTime(),
			size:    info.Size(),
		}
		return nil
	})
	return state, err
}

// updateFileState returns the paths that changed since the last poll.
func updateFileState(state map[string]fileState) []string {
	var changed []string
	for path, old := range state {
		info, err := os.Stat(path)
		if err != nil {
			delete(state, path)
			changed = append(changed, path)
			continue
		}
		next := fileState{modTime: info.ModTime(), size: info.Size()}
		if next.modTime != old.modTime || next.size != old.size {
			state[path] = next
			changed = append(changed, path)
		}
	}
	return changed
}

// diffFileState returns the paths that changed between two snapshots.
func diffFileState(prev, next map[string]fileState) []string {
	var changed []string
	for path, old := range prev {
		cur, ok := next[path]
		if !ok {
			changed = append(changed, path)
			continue
		}
		if old.modTime != cur.modTime || old.size != cur.size {
			changed = append(changed, path)
		}
	}
	for path := range next {
		if _, ok := prev[path]; !ok {
			changed = append(changed, path)
		}
	}
	return changed
}

// shouldSkipDir reports whether a directory should be ignored for watching.
func shouldSkipDir(name string) bool {
	if name == "vendor" {
		return true
	}
	return strings.HasPrefix(name, ".")
}

// formatChangedFiles formats a list of changed paths relative to root.
func formatChangedFiles(paths []string, root string) string {
	if len(paths) == 0 {
		return "no files"
	}
	relPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			relPaths = append(relPaths, path)
			continue
		}
		relPaths = append(relPaths, rel)
	}
	if len(paths) == 1 {
		return relPaths[0]
	}
	limit := 5
	if len(paths) < limit {
		limit = len(paths)
	}
	return strings.Join(relPaths[:limit], ", ") + formatRemaining(len(paths)-limit)
}

// formatRemaining formats the "+N more" suffix for truncated lists.
func formatRemaining(remaining int) string {
	if remaining <= 0 {
		return ""
	}
	return " +" + strconv.Itoa(remaining) + " more"
}

// moduleRoot resolves the module root for the current working directory.
func moduleRoot(wd string, env []string) (string, error) {
	cmd := exec.Command("go", "env", "GOMOD")
	cmd.Dir = wd
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" || path == os.DevNull {
		return wd, nil
	}
	return filepath.Dir(path), nil
}

// formatDuration renders a short millisecond duration for log output.
func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
}

// watchWithFSNotify runs the watcher using native filesystem notifications.
func watchWithFSNotify(root string, onChange func()) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := addWatchDirs(watcher, root); err != nil {
		return err
	}

	changed := make(map[string]struct{})
	debounce := 200 * time.Millisecond
	timer := time.NewTimer(debounce)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("watcher closed")
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if !shouldSkipDir(filepath.Base(event.Name)) {
						_ = addWatchDirs(watcher, event.Name)
					}
					continue
				}
			}
			if !isWatchedGoFile(event.Name) {
				continue
			}
			changed[event.Name] = struct{}{}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)
		case <-timer.C:
			if len(changed) == 0 {
				continue
			}
			paths := make([]string, 0, len(changed))
			for path := range changed {
				paths = append(paths, path)
			}
			for key := range changed {
				delete(changed, key)
			}
			log.Printf("watch: changes detected (%s), re-running", formatChangedFiles(paths, root))
			onChange()
		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher closed")
			}
			return err
		}
	}
}

// addWatchDirs registers watchers for all directories under root.
func addWatchDirs(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if err := watcher.Add(path); err != nil {
			return err
		}
		return nil
	})
}

// isWatchedGoFile reports whether a path should trigger a regeneration.
func isWatchedGoFile(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	return !strings.HasSuffix(path, "wire_gen.go")
}
