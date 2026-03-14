package main

import (
	"bytes"
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
	want := "wire: type-check failed for example.com/app/app\n"
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
	want := ansiRed + "wire: type-check failed for example.com/app/app\n" + ansiReset
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
	want := ansiRed + "wire: \n" + ansiReset +
		ansiRed + "    first line\n" + ansiReset +
		ansiRed + "    second line\n" + ansiReset
	if got != want {
		t.Fatalf("writeErrorLog() = %q, want %q", got, want)
	}
}

type testError string

func (e testError) Error() string { return string(e) }
