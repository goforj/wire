package wire

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// validateGenerateCommand rejects prefixes that go generate cannot tokenize or execute.
func validateGenerateCommand(command string) error {
	if command == "" {
		return nil
	}
	if !utf8.ValidString(command) || strings.ContainsAny(command, "\r\n\x00") {
		return fmt.Errorf("generate_command must be valid UTF-8 on one line without NUL bytes")
	}
	rest := strings.TrimLeft(command, " \t")
	if rest == "" {
		return fmt.Errorf("generate_command must contain an executable")
	}
	first := true
	for rest != "" {
		var word string
		if rest[0] == '"' {
			quoted, err := strconv.QuotedPrefix(rest)
			if err != nil {
				return fmt.Errorf("generate_command contains an invalid quoted argument: %w", err)
			}
			word, _ = strconv.Unquote(quoted)
			rest = rest[len(quoted):]
			if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
				return fmt.Errorf("generate_command requires whitespace after a quoted argument")
			}
		} else {
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				end = len(rest)
			}
			word, rest = rest[:end], rest[end:]
		}
		if strings.ContainsRune(word, '\x00') || (first && word == "") {
			return fmt.Errorf("generate_command requires a nonempty executable and arguments without NUL bytes")
		}
		first = false
		rest = strings.TrimLeft(rest, " \t")
	}
	return nil
}

// quoteGenerateArgument preserves literal dollars through go generate's environment expansion.
func quoteGenerateArgument(value string) string {
	return strconv.Quote(strings.ReplaceAll(value, "$", "${DOLLAR}"))
}
