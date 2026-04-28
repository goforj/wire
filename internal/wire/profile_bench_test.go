package wire

import (
	"context"
	"os"
	"testing"
)

func BenchmarkGenerateRealAppWarmArtifacts(b *testing.B) {
	root := os.Getenv("WIRE_REAL_APP_ROOT")
	if root == "" {
		b.Skip("WIRE_REAL_APP_ROOT not set")
	}
	artifactDir := b.TempDir()
	env := append(os.Environ(),
		"WIRE_LOADER_ARTIFACTS=1",
		"WIRE_LOADER_ARTIFACT_DIR="+artifactDir,
	)
	ctx := context.Background()

	// Warm the artifact cache once before measurement.
	if _, errs := Generate(ctx, root, env, []string{"."}, &GenerateOptions{}); len(errs) > 0 {
		b.Fatalf("warm Generate errors: %v", errs)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, errs := Generate(ctx, root, env, []string{"."}, &GenerateOptions{}); len(errs) > 0 {
			b.Fatalf("Generate errors: %v", errs)
		}
	}
}
