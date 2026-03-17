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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/subcommands"
)

const (
	loaderArtifactDirEnv = "WIRE_LOADER_ARTIFACT_DIR"
	outputCacheDirEnv    = "WIRE_OUTPUT_CACHE_DIR"
	semanticCacheDirEnv  = "WIRE_SEMANTIC_CACHE_DIR"
)

var osUserCacheDir = os.UserCacheDir

type cacheCmd struct {
	clear bool
}

type cacheTarget struct {
	name string
	path string
}

func (*cacheCmd) Name() string { return "cache" }

func (*cacheCmd) Synopsis() string {
	return "inspect or clear the wire cache"
}

func (*cacheCmd) Usage() string {
	return `cache
cache clear
cache -clear

  By default, prints the cache directory. With -clear or clear, removes all
  Wire-managed cache files.
`
}

func (cmd *cacheCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&cmd.clear, "clear", false, "clear Wire caches")
}

func (cmd *cacheCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	_ = ctx
	clearRequested := cmd.clear
	switch extra := f.Args(); len(extra) {
	case 0:
		if !clearRequested {
			root, err := wireCacheRoot(os.Environ())
			if err != nil {
				log.Println(err)
				return subcommands.ExitFailure
			}
			fmt.Fprintln(os.Stdout, root)
			return subcommands.ExitSuccess
		}
	case 1:
		if extra[0] == "clear" {
			clearRequested = true
			break
		}
		log.Printf("unknown cache action %q", extra[0])
		log.Println(strings.TrimSpace(cmd.Usage()))
		return subcommands.ExitFailure
	default:
		log.Println(strings.TrimSpace(cmd.Usage()))
		return subcommands.ExitFailure
	}
	if !clearRequested {
		log.Println(strings.TrimSpace(cmd.Usage()))
		return subcommands.ExitFailure
	}
	cleared, err := clearWireCaches(os.Environ())
	if err != nil {
		log.Printf("failed to clear cache: %v\n", err)
		return subcommands.ExitFailure
	}
	root, err := wireCacheRoot(os.Environ())
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}
	if len(cleared) == 0 {
		log.Printf("cleared cache at %s\n", root)
		return subcommands.ExitSuccess
	}
	log.Printf("cleared cache at %s\n", root)
	return subcommands.ExitSuccess
}

func wireCacheRoot(env []string) (string, error) {
	base, err := osUserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(base, "wire"), nil
}

func clearWireCaches(env []string) ([]string, error) {
	base, err := wireCacheRoot(env)
	if err != nil {
		return nil, err
	}
	targets := wireCacheTargets(env, filepath.Dir(base))
	cleared := make([]string, 0, len(targets))
	for _, target := range targets {
		info, err := os.Stat(target.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return cleared, fmt.Errorf("stat %s cache: %w", target.name, err)
		}
		if !info.IsDir() {
			if err := os.Remove(target.path); err != nil {
				return cleared, fmt.Errorf("remove %s cache: %w", target.name, err)
			}
		} else if err := os.RemoveAll(target.path); err != nil {
			return cleared, fmt.Errorf("remove %s cache: %w", target.name, err)
		}
		cleared = append(cleared, target.name)
	}
	return cleared, nil
}

func wireCacheTargets(env []string, userCacheDir string) []cacheTarget {
	baseWire := filepath.Join(userCacheDir, "wire")
	targets := []cacheTarget{
		{name: "loader-artifacts", path: envValueDefault(env, loaderArtifactDirEnv, filepath.Join(baseWire, "loader-artifacts"))},
		{name: "discovery-cache", path: filepath.Join(baseWire, "discovery-cache")},
		{name: "output-cache", path: envValueDefault(env, outputCacheDirEnv, filepath.Join(baseWire, "output-cache"))},
	}
	seen := make(map[string]bool, len(targets))
	deduped := make([]cacheTarget, 0, len(targets))
	for _, target := range targets {
		cleaned := filepath.Clean(target.path)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		target.path = cleaned
		deduped = append(deduped, target)
	}
	sort.Slice(deduped, func(i, j int) bool { return deduped[i].name < deduped[j].name })
	return deduped
}

func envValueDefault(env []string, key, fallback string) string {
	for i := len(env) - 1; i >= 0; i-- {
		parts := strings.SplitN(env[i], "=", 2)
		if len(parts) == 2 && parts[0] == key && parts[1] != "" {
			return parts[1]
		}
	}
	return fallback
}
