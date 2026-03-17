package wire

import (
	"context"
	"testing"
)

func TestOutputCacheEnabled(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		env  []string
		want bool
	}{
		{
			name: "enabled with artifacts",
			env:  []string{"WIRE_LOADER_ARTIFACTS=1"},
			want: true,
		},
		{
			name: "disabled without artifacts",
			env:  []string{"WIRE_LOADER_ARTIFACTS=0"},
			want: false,
		},
		{
			name: "disabled by dedicated env",
			env:  []string{"WIRE_LOADER_ARTIFACTS=1", "WIRE_OUTPUT_CACHE=0"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outputCacheEnabled(ctx, t.TempDir(), tt.env); got != tt.want {
				t.Fatalf("outputCacheEnabled(..., %v) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}
