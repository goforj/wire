package wire

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/goforj/wire/internal/loader"
)

const outputCacheDirEnv = "WIRE_OUTPUT_CACHE_DIR"

type outputCacheEntry struct {
	Version int
	Content []byte
}

type outputCacheCandidate struct {
	path       string
	outputPath string
}

func prepareGenerateOutputCache(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions) (map[string]outputCacheCandidate, []GenerateResult, bool) {
	if !outputCacheEnabled(ctx, wd, env) {
		debugf(ctx, "generate.output_cache=disabled")
		return nil, nil, false
	}
	rootResult, err := loader.New().LoadRootGraph(withLoaderTiming(ctx), loader.RootLoadRequest{
		WD:       wd,
		Env:      env,
		Tags:     opts.Tags,
		Patterns: append([]string(nil), patterns...),
		NeedDeps: true,
		Mode:     effectiveLoaderMode(ctx, wd, env),
	})
	if err != nil || rootResult == nil || len(rootResult.Packages) == 0 {
		if err != nil {
			debugf(ctx, "generate.output_cache=load_root_error")
		} else {
			debugf(ctx, "generate.output_cache=no_roots")
		}
		return nil, nil, false
	}
	candidates := make(map[string]outputCacheCandidate, len(rootResult.Packages))
	results := make([]GenerateResult, 0, len(rootResult.Packages))
	for _, pkg := range rootResult.Packages {
		outDir, err := detectOutputDir(pkg.GoFiles)
		if err != nil {
			debugf(ctx, "generate.output_cache=bad_output_dir")
			return candidates, nil, false
		}
		key, err := outputCacheKey(wd, opts, pkg)
		if err != nil {
			debugf(ctx, "generate.output_cache=key_error")
			return candidates, nil, false
		}
		path, err := outputCachePath(env, key)
		if err != nil {
			debugf(ctx, "generate.output_cache=path_error")
			return candidates, nil, false
		}
		candidates[pkg.PkgPath] = outputCacheCandidate{
			path:       path,
			outputPath: filepath.Join(outDir, opts.PrefixOutputFile+"wire_gen.go"),
		}
		entry, ok := readOutputCache(path)
		if !ok {
			debugf(ctx, "generate.output_cache=miss")
			return candidates, nil, false
		}
		results = append(results, GenerateResult{
			PkgPath:    pkg.PkgPath,
			OutputPath: filepath.Join(outDir, opts.PrefixOutputFile+"wire_gen.go"),
			Content:    entry.Content,
		})
	}
	debugf(ctx, "generate.output_cache=hit")
	return candidates, results, len(results) == len(rootResult.Packages)
}

func writeGenerateOutputCache(candidates map[string]outputCacheCandidate, generated []GenerateResult) {
	for _, gen := range generated {
		candidate, ok := candidates[gen.PkgPath]
		if !ok || candidate.path == "" || len(gen.Errs) > 0 || len(gen.Content) == 0 {
			continue
		}
		_ = writeOutputCache(candidate.path, &outputCacheEntry{
			Version: 1,
			Content: append([]byte(nil), gen.Content...),
		})
	}
}

func outputCacheEnabled(ctx context.Context, wd string, env []string) bool {
	if effectiveLoaderMode(ctx, wd, env) == loader.ModeFallback {
		return false
	}
	return envValue(env, "WIRE_LOADER_ARTIFACTS") == "1"
}

func outputCachePath(env []string, key string) (string, error) {
	dir, err := outputCacheDir(env)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, key+".gob"), nil
}

func outputCacheDir(env []string) (string, error) {
	if dir := envValue(env, outputCacheDirEnv); dir != "" {
		return dir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "wire", "output-cache"), nil
}

func readOutputCache(path string) (*outputCacheEntry, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var entry outputCacheEntry
	if err := gob.NewDecoder(f).Decode(&entry); err != nil {
		return nil, false
	}
	if entry.Version != 1 {
		return nil, false
	}
	return &entry, true
}

func writeOutputCache(path string, entry *outputCacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewEncoder(f).Encode(entry)
}

func outputCacheKey(wd string, opts *GenerateOptions, root *packages.Package) (string, error) {
	sum := sha256.New()
	sum.Write([]byte("wire-output-cache-v1\n"))
	sum.Write([]byte(runtime.Version()))
	sum.Write([]byte{'\n'})
	sum.Write([]byte(canonicalWirePath(wd)))
	sum.Write([]byte{'\n'})
	sum.Write(opts.Header)
	sum.Write([]byte{'\n'})
	sum.Write([]byte(opts.Tags))
	sum.Write([]byte{'\n'})
	sum.Write([]byte(root.PkgPath))
	sum.Write([]byte{'\n'})
	workspace := detectWireModuleRoot(wd)
	pkgs := reachablePackages(root)
	for _, pkg := range pkgs {
		sum.Write([]byte(pkg.PkgPath))
		sum.Write([]byte{'\n'})
		if isLocalWirePackage(workspace, pkg) {
			files := append([]string(nil), pkg.GoFiles...)
			sort.Strings(files)
			for _, name := range files {
				info, err := os.Stat(name)
				if err != nil {
					return "", err
				}
				sum.Write([]byte(name))
				sum.Write([]byte{'\n'})
				sum.Write([]byte(strconv.FormatInt(info.Size(), 10)))
				sum.Write([]byte{'\n'})
				sum.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
				sum.Write([]byte{'\n'})
				if pkg.PkgPath == root.PkgPath {
					src, err := os.ReadFile(name)
					if err != nil {
						return "", err
					}
					sum.Write(src)
					sum.Write([]byte{'\n'})
				}
			}
			continue
		}
		sum.Write([]byte(pkg.ExportFile))
		sum.Write([]byte{'\n'})
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func reachablePackages(root *packages.Package) []*packages.Package {
	seen := map[string]bool{}
	var out []*packages.Package
	var walk func(*packages.Package)
	walk = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.PkgPath] {
			return
		}
		seen[pkg.PkgPath] = true
		out = append(out, pkg)
		paths := make([]string, 0, len(pkg.Imports))
		for path := range pkg.Imports {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			walk(pkg.Imports[path])
		}
	}
	walk(root)
	sort.Slice(out, func(i, j int) bool { return out[i].PkgPath < out[j].PkgPath })
	return out
}

func isLocalWirePackage(workspace string, pkg *packages.Package) bool {
	if pkg == nil || len(pkg.GoFiles) == 0 {
		return false
	}
	dir := filepath.Dir(pkg.GoFiles[0])
	dir = canonicalWirePath(dir)
	workspace = canonicalWirePath(workspace)
	if dir == workspace {
		return true
	}
	return len(dir) > len(workspace) && dir[:len(workspace)] == workspace && dir[len(workspace)] == filepath.Separator
}

func detectWireModuleRoot(start string) string {
	start = canonicalWirePath(start)
	for dir := start; dir != "" && dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return start
}

func canonicalWirePath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return path
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		name, value, ok := strings.Cut(env[i], "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}
