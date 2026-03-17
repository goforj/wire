package wire

import "testing"

func TestSemanticReconstructionEnabled(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want bool
	}{
		{
			name: "disabled by default",
			want: false,
		},
		{
			name: "enabled by env",
			env:  []string{"WIRE_SEMANTIC_RECONSTRUCTION=1"},
			want: true,
		},
		{
			name: "disabled by env",
			env:  []string{"WIRE_SEMANTIC_RECONSTRUCTION=0"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := semanticReconstructionEnabled(tt.env); got != tt.want {
				t.Fatalf("semanticReconstructionEnabled(%v) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}
