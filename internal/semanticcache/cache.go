package semanticcache

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

const dirEnv = "WIRE_SEMANTIC_CACHE_DIR"

type PackageArtifact struct {
	Version            int
	PackagePath        string
	PackageName        string
	HasProviderSetVars bool
	Supported          bool
	Vars               map[string]ProviderSetArtifact
}

type ProviderSetArtifact struct {
	Items []ProviderSetItemArtifact
}

type ProviderSetItemArtifact struct {
	Kind       string
	ImportPath string
	Name       string
	Type       TypeRef
	Type2      TypeRef
	FieldNames []string
	AllFields  bool
}

type TypeRef struct {
	ImportPath string
	Name       string
	Pointer    int
}

func ArtifactPath(env []string, importPath, packageName string, files []string) (string, error) {
	dir, err := artifactDir(env)
	if err != nil {
		return "", err
	}
	key, err := artifactKey(importPath, packageName, files)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, key+".gob"), nil
}

func Read(env []string, importPath, packageName string, files []string) (*PackageArtifact, error) {
	path, err := ArtifactPath(env, importPath, packageName, files)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var art PackageArtifact
	if err := gob.NewDecoder(f).Decode(&art); err != nil {
		return nil, err
	}
	return &art, nil
}

func Write(env []string, importPath, packageName string, files []string, art *PackageArtifact) error {
	path, err := ArtifactPath(env, importPath, packageName, files)
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
	return gob.NewEncoder(f).Encode(art)
}

func Exists(env []string, importPath, packageName string, files []string) bool {
	path, err := ArtifactPath(env, importPath, packageName, files)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func artifactDir(env []string) (string, error) {
	for i := len(env) - 1; i >= 0; i-- {
		key, val, ok := splitEnv(env[i])
		if ok && key == dirEnv && val != "" {
			return val, nil
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "wire", "semantic-artifacts"), nil
}

func artifactKey(importPath, packageName string, files []string) (string, error) {
	sum := sha256.New()
	sum.Write([]byte("wire-semantic-artifact-v1\n"))
	sum.Write([]byte(runtime.Version()))
	sum.Write([]byte{'\n'})
	sum.Write([]byte(importPath))
	sum.Write([]byte{'\n'})
	sum.Write([]byte(packageName))
	sum.Write([]byte{'\n'})
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
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func splitEnv(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}
