package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	ansiRed             = "\033[1;31m"
	ansiGreen           = "\033[1;32m"
	ansiReset           = "\033[0m"
	successSig          = "✓ "
	errorSig            = "x "
	maxLoggedErrorLines = 5
)

func logErrors(errs []error) {
	for _, err := range errs {
		msg := truncateLoggedError(formatLoggedError(err))
		if strings.Contains(msg, "\n") {
			logMultilineError("\n    " + strings.ReplaceAll(msg, "\n", "\n    "))
			continue
		}
		logMultilineError(msg)
	}
}

func formatLoggedError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "inject ") {
		return "solve failed\n" + msg
	}
	if idx := strings.Index(msg, ": inject "); idx >= 0 {
		return "solve failed\n" + msg
	}
	return msg
}

func truncateLoggedError(msg string) string {
	if msg == "" {
		return ""
	}
	lines := strings.Split(msg, "\n")
	if len(lines) <= maxLoggedErrorLines {
		return msg
	}
	omitted := len(lines) - maxLoggedErrorLines
	lines = append(lines[:maxLoggedErrorLines], fmt.Sprintf("... (%d additional lines omitted)", omitted))
	return strings.Join(lines, "\n")
}

func logMultilineError(msg string) {
	writeErrorLog(os.Stderr, msg)
}

func logSuccessf(format string, args ...interface{}) {
	writeStatusLog(os.Stderr, fmt.Sprintf(format, args...))
}

func shouldColorStderr() bool {
	return shouldColorOutput(stderrIsTTY(), os.Getenv("TERM"))
}

func shouldColorOutput(isTTY bool, term string) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("CLICOLOR") == "0" {
		return false
	}
	if forceColorEnabled() {
		return true
	}
	if term == "" || term == "dumb" {
		return false
	}
	return isTTY
}

func forceColorEnabled() bool {
	return os.Getenv("FORCE_COLOR") != "" || os.Getenv("CLICOLOR_FORCE") != ""
}

func stderrIsTTY() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func writeErrorLog(w io.Writer, msg string) {
	line := errorSig + "wire: " + msg
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	if shouldColorStderr() {
		_, _ = io.WriteString(w, colorizeLines(line))
		return
	}
	_, _ = io.WriteString(w, line)
}

func writeStatusLog(w io.Writer, msg string) {
	line := successSig + "wire: " + msg
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	if shouldColorStderr() {
		_, _ = io.WriteString(w, ansiGreen+line+ansiReset)
		return
	}
	_, _ = io.WriteString(w, line)
}

func colorizeLines(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.SplitAfter(s, "\n")
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteString(ansiRed)
		b.WriteString(part)
		b.WriteString(ansiReset)
	}
	return b.String()
}
