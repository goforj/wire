package loader

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

type discoveryCacheEntry struct {
	Version   int
	WD        string
	Tags      string
	Patterns  []string
	NeedDeps  bool
	Workspace string
	Meta      map[string]*packageMeta
	Global    []discoveryFileMeta
	LocalPkgs []discoveryLocalPackage
}

type discoveryLocalPackage struct {
	ImportPath string
	Dir        string
	DirMeta    discoveryDirMeta
	Files      []discoveryFileFingerprint
}

type discoveryFileMeta struct {
	Path    string
	Size    int64
	ModTime int64
	IsDir   bool
}

type discoveryDirMeta struct {
	Path    string
	Entries []string
}

type discoveryFileFingerprint struct {
	Path string
	Hash string
}

func readDiscoveryCache(req goListRequest) (map[string]*packageMeta, bool) {
	entry, err := loadDiscoveryCacheEntry(req)
	if err != nil || entry == nil {
		return nil, false
	}
	if !validateDiscoveryCacheEntry(entry) {
		return nil, false
	}
	return clonePackageMetaMap(entry.Meta), true
}

func writeDiscoveryCache(req goListRequest, meta map[string]*packageMeta) {
	entry, err := buildDiscoveryCacheEntry(req, meta)
	if err != nil {
		return
	}
	_ = saveDiscoveryCacheEntry(req, entry)
}

func buildDiscoveryCacheEntry(req goListRequest, meta map[string]*packageMeta) (*discoveryCacheEntry, error) {
	workspace := detectModuleRoot(req.WD)
	entry := &discoveryCacheEntry{
		Version:   2,
		WD:        canonicalLoaderPath(req.WD),
		Tags:      req.Tags,
		Patterns:  append([]string(nil), req.Patterns...),
		NeedDeps:  req.NeedDeps,
		Workspace: workspace,
		Meta:      clonePackageMetaMap(meta),
	}
	global := []string{
		filepath.Join(workspace, "go.mod"),
		filepath.Join(workspace, "go.sum"),
		filepath.Join(workspace, "go.work"),
		filepath.Join(workspace, "go.work.sum"),
	}
	for _, name := range global {
		if fm, ok := statDiscoveryFile(name); ok {
			entry.Global = append(entry.Global, fm)
		}
	}
	locals := make([]discoveryLocalPackage, 0)
	for _, pkg := range meta {
		if pkg == nil || !isWorkspacePackage(workspace, pkg.Dir) {
			continue
		}
		lp := discoveryLocalPackage{
			ImportPath: pkg.ImportPath,
			Dir:        pkg.Dir,
		}
		if fm, ok := statDiscoveryDir(pkg.Dir); ok {
			lp.DirMeta = fm
		}
		for _, name := range metaFiles(pkg) {
			if fm, ok := fingerprintDiscoveryFile(name); ok {
				lp.Files = append(lp.Files, fm)
			}
		}
		sort.Slice(lp.Files, func(i, j int) bool { return lp.Files[i].Path < lp.Files[j].Path })
		locals = append(locals, lp)
	}
	sort.Slice(locals, func(i, j int) bool { return locals[i].ImportPath < locals[j].ImportPath })
	entry.LocalPkgs = locals
	return entry, nil
}

func validateDiscoveryCacheEntry(entry *discoveryCacheEntry) bool {
	if entry == nil || entry.Version != 2 {
		return false
	}
	for _, fm := range entry.Global {
		if !matchesDiscoveryFile(fm) {
			return false
		}
	}
	for _, lp := range entry.LocalPkgs {
		if !matchesDiscoveryDir(lp.DirMeta) {
			return false
		}
		for _, fm := range lp.Files {
			if !matchesDiscoveryFingerprint(fm) {
				return false
			}
		}
	}
	return true
}

func discoveryCachePath(req goListRequest) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sumReq := struct {
		Version  int
		WD       string
		Tags     string
		Patterns []string
		NeedDeps bool
		Go       string
	}{
		Version:  2,
		WD:       canonicalLoaderPath(req.WD),
		Tags:     req.Tags,
		Patterns: append([]string(nil), req.Patterns...),
		NeedDeps: req.NeedDeps,
		Go:       runtime.Version(),
	}
	key, err := hashGob(sumReq)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "wire", "discovery-cache", key+".gob"), nil
}

func loadDiscoveryCacheEntry(req goListRequest) (*discoveryCacheEntry, error) {
	path, err := discoveryCachePath(req)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entry discoveryCacheEntry
	if err := gob.NewDecoder(f).Decode(&entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func saveDiscoveryCacheEntry(req goListRequest, entry *discoveryCacheEntry) error {
	path, err := discoveryCachePath(req)
	if err != nil {
		return err
	}
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

func statDiscoveryFile(path string) (discoveryFileMeta, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return discoveryFileMeta{}, false
	}
	return discoveryFileMeta{
		Path:    canonicalLoaderPath(path),
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
		IsDir:   info.IsDir(),
	}, true
}

func matchesDiscoveryFile(fm discoveryFileMeta) bool {
	cur, ok := statDiscoveryFile(fm.Path)
	if !ok {
		return false
	}
	return cur.Size == fm.Size && cur.ModTime == fm.ModTime && cur.IsDir == fm.IsDir
}

func statDiscoveryDir(path string) (discoveryDirMeta, bool) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return discoveryDirMeta{}, false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return discoveryDirMeta{
		Path:    canonicalLoaderPath(path),
		Entries: names,
	}, true
}

func matchesDiscoveryDir(dm discoveryDirMeta) bool {
	cur, ok := statDiscoveryDir(dm.Path)
	if !ok {
		return false
	}
	if len(cur.Entries) != len(dm.Entries) {
		return false
	}
	for i := range cur.Entries {
		if cur.Entries[i] != dm.Entries[i] {
			return false
		}
	}
	return true
}

func fingerprintDiscoveryFile(path string) (discoveryFileFingerprint, bool) {
	src, err := os.ReadFile(path)
	if err != nil {
		return discoveryFileFingerprint{}, false
	}
	sum := sha256.New()
	sum.Write([]byte(filepath.Base(path)))
	sum.Write([]byte{0})
	file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		sum.Write(src)
		return discoveryFileFingerprint{
			Path: canonicalLoaderPath(path),
			Hash: hex.EncodeToString(sum.Sum(nil)),
		}, true
	}
	if offset := int(file.Package) - 1; offset > 0 && offset <= len(src) {
		sum.Write(src[:offset])
	}
	sum.Write([]byte(file.Name.Name))
	sum.Write([]byte{0})
	for _, imp := range file.Imports {
		if imp.Name != nil {
			sum.Write([]byte(imp.Name.Name))
		}
		sum.Write([]byte{0})
		sum.Write([]byte(imp.Path.Value))
		sum.Write([]byte{0})
	}
	return discoveryFileFingerprint{
		Path: canonicalLoaderPath(path),
		Hash: hex.EncodeToString(sum.Sum(nil)),
	}, true
}

func matchesDiscoveryFingerprint(fp discoveryFileFingerprint) bool {
	cur, ok := fingerprintDiscoveryFile(fp.Path)
	if !ok {
		return false
	}
	return cur.Hash == fp.Hash
}

func hashGob(v interface{}) (string, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func clonePackageMetaMap(in map[string]*packageMeta) map[string]*packageMeta {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*packageMeta, len(in))
	for k, v := range in {
		if v == nil {
			continue
		}
		cp := *v
		cp.GoFiles = append([]string(nil), v.GoFiles...)
		cp.CompiledGoFiles = append([]string(nil), v.CompiledGoFiles...)
		cp.Imports = append([]string(nil), v.Imports...)
		if v.ImportMap != nil {
			cp.ImportMap = make(map[string]string, len(v.ImportMap))
			for mk, mv := range v.ImportMap {
				cp.ImportMap[mk] = mv
			}
		}
		if v.Error != nil {
			errCopy := *v.Error
			cp.Error = &errCopy
		}
		out[k] = &cp
	}
	return out
}
