package wire

import (
	"path/filepath"
	"sort"
	"strings"
)

func packageCacheScope(wd string) string {
	if root := findModuleRoot(wd); root != "" {
		return filepath.Clean(root)
	}
	return filepath.Clean(wd)
}

func runCacheScope(wd string, patterns []string) string {
	scopeRoot := packageCacheScope(wd)
	normalized := normalizePatternsForScope(wd, scopeRoot, patterns)
	if len(normalized) == 0 {
		return scopeRoot
	}
	return scopeRoot + "\n" + strings.Join(normalized, "\n")
}

func normalizePatternsForScope(wd string, scopeRoot string, patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		out = append(out, normalizePatternForScope(wd, scopeRoot, pattern))
	}
	sort.Strings(out)
	return out
}

func normalizePatternForScope(wd string, scopeRoot string, pattern string) string {
	if pattern == "" {
		return pattern
	}
	if filepath.IsAbs(pattern) || strings.HasPrefix(pattern, ".") {
		abs := pattern
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(wd, pattern)
		}
		abs = filepath.Clean(abs)
		if scopeRoot != "" {
			if rel, ok := pathWithinRoot(scopeRoot, abs); ok {
				if rel == "." {
					return "."
				}
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(abs)
	}
	return pattern
}

func pathWithinRoot(root string, path string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}
