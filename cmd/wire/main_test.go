package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestFormatLoggedErrorAddsSolveHeader(t *testing.T) {
	err := testError("inject InitializeApplication: no provider found for *example.Foo")
	got := formatLoggedError(err)
	want := "solve failed\ninject InitializeApplication: no provider found for *example.Foo"
	if got != want {
		t.Fatalf("formatLoggedError() = %q, want %q", got, want)
	}
}

func TestFormatLoggedErrorAddsSolveHeaderWithPositionPrefix(t *testing.T) {
	err := testError("/tmp/wire.go:12:1: inject InitializeApplication: no provider found for *example.Foo")
	got := formatLoggedError(err)
	want := "solve failed\n/tmp/wire.go:12:1: inject InitializeApplication: no provider found for *example.Foo"
	if got != want {
		t.Fatalf("formatLoggedError() = %q, want %q", got, want)
	}
}

func TestFormatLoggedErrorLeavesNonSolveErrorsUnchanged(t *testing.T) {
	err := testError("type-check failed for example.com/app/app")
	got := formatLoggedError(err)
	if got != err.Error() {
		t.Fatalf("formatLoggedError() = %q, want %q", got, err.Error())
	}
}

func TestTruncateLoggedErrorSummarizesLargeBlocks(t *testing.T) {
	lines := make([]string, 0, maxLoggedErrorLines+3)
	for i := 0; i < maxLoggedErrorLines+3; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	got := truncateLoggedError(strings.Join(lines, "\n"))
	wantLines := append(append([]string(nil), lines[:maxLoggedErrorLines]...), "... (3 additional lines omitted)")
	want := strings.Join(wantLines, "\n")
	if got != want {
		t.Fatalf("truncateLoggedError() = %q, want %q", got, want)
	}
}

func TestShouldColorOutputForceColorOverridesTTYRequirement(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")

	if !shouldColorOutput(false, "xterm-256color") {
		t.Fatal("shouldColorOutput() = false, want true when FORCE_COLOR is set")
	}
}

func TestShouldColorOutputNoColorWins(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")

	if shouldColorOutput(true, "xterm-256color") {
		t.Fatal("shouldColorOutput() = true, want false when NO_COLOR is set")
	}
}

func TestShouldColorOutputTTYFallback(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")

	if !shouldColorOutput(true, "xterm-256color") {
		t.Fatal("shouldColorOutput() = false, want true for tty stderr")
	}
	if shouldColorOutput(false, "xterm-256color") {
		t.Fatal("shouldColorOutput() = true, want false for non-tty stderr without force color")
	}
}

func TestWriteErrorLogFormatsWirePrefix(t *testing.T) {
	var buf bytes.Buffer
	writeErrorLog(&buf, "type-check failed for example.com/app/app")
	got := buf.String()
	want := errorSig + "wire: type-check failed for example.com/app/app\n"
	if got != want {
		t.Fatalf("writeErrorLog() = %q, want %q", got, want)
	}
}

func TestWriteErrorLogColorsWholeBlockWhenForced(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")

	var buf bytes.Buffer
	writeErrorLog(&buf, "type-check failed for example.com/app/app")
	got := buf.String()
	want := ansiRed + errorSig + "wire: type-check failed for example.com/app/app\n" + ansiReset
	if got != want {
		t.Fatalf("writeErrorLog() = %q, want %q", got, want)
	}
}

func TestWriteErrorLogColorsEachMultilineLineWhenForced(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")

	var buf bytes.Buffer
	writeErrorLog(&buf, "\n    first line\n    second line")
	got := buf.String()
	want := ansiRed + errorSig + "wire: \n" + ansiReset +
		ansiRed + "    first line\n" + ansiReset +
		ansiRed + "    second line\n" + ansiReset
	if got != want {
		t.Fatalf("writeErrorLog() = %q, want %q", got, want)
	}
}

func TestWriteStatusLogFormatsSuccessPrefix(t *testing.T) {
	var buf bytes.Buffer
	writeStatusLog(&buf, "example.com/app: wrote /tmp/wire_gen.go (12ms)")
	got := buf.String()
	want := successSig + "wire: example.com/app: wrote /tmp/wire_gen.go (12ms)\n"
	if got != want {
		t.Fatalf("writeStatusLog() = %q, want %q", got, want)
	}
}

type testError string

func (e testError) Error() string { return string(e) }
