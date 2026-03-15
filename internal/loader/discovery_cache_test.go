package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryFingerprintIgnoresBodyOnlyEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg.go")
	before := `package example

import "fmt"

func Provide() string {
	return fmt.Sprint("before")
}
`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	fpBefore, ok := fingerprintDiscoveryFile(path)
	if !ok {
		t.Fatalf("fingerprintDiscoveryFile(%q) failed", path)
	}
	after := `package example

import "fmt"

func Provide() string {
	return fmt.Sprint("after")
}
`
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	fpAfter, ok := fingerprintDiscoveryFile(path)
	if !ok {
		t.Fatalf("fingerprintDiscoveryFile(%q) failed after body edit", path)
	}
	if fpBefore.Hash != fpAfter.Hash {
		t.Fatalf("body-only edit changed fingerprint: %s != %s", fpBefore.Hash, fpAfter.Hash)
	}
}

func TestDiscoveryFingerprintDetectsImportChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg.go")
	before := `package example

import "fmt"
`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	fpBefore, ok := fingerprintDiscoveryFile(path)
	if !ok {
		t.Fatalf("fingerprintDiscoveryFile(%q) failed", path)
	}
	after := `package example

import "strings"
`
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	fpAfter, ok := fingerprintDiscoveryFile(path)
	if !ok {
		t.Fatalf("fingerprintDiscoveryFile(%q) failed after import edit", path)
	}
	if fpBefore.Hash == fpAfter.Hash {
		t.Fatalf("import edit did not change fingerprint")
	}
}

func TestDiscoveryFingerprintDetectsHeaderBuildTagChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg.go")
	before := `//go:build linux

package example

import "fmt"
`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	fpBefore, ok := fingerprintDiscoveryFile(path)
	if !ok {
		t.Fatalf("fingerprintDiscoveryFile(%q) failed", path)
	}
	after := `//go:build darwin

package example

import "fmt"
`
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	fpAfter, ok := fingerprintDiscoveryFile(path)
	if !ok {
		t.Fatalf("fingerprintDiscoveryFile(%q) failed after header edit", path)
	}
	if fpBefore.Hash == fpAfter.Hash {
		t.Fatalf("build tag edit did not change fingerprint")
	}
}

func TestDiscoveryDirDetectsFileSetChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, ok := statDiscoveryDir(dir)
	if !ok {
		t.Fatalf("statDiscoveryDir(%q) failed", dir)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if matchesDiscoveryDir(before) {
		t.Fatalf("directory metadata did not detect added file")
	}
}
