package cachepolicy

import "os"

const (
	ModeEnv     = "WIRE_CACHE_MODE"
	ModeMTime   = "mtime"
	ModeContent = "content"
)

var mode = loadMode()

func UseFileContent() bool {
	return mode == ModeContent
}

func ModeLabel() string {
	return mode
}

func SetForTest(next string) func() {
	prev := mode
	mode = normalizeMode(next)
	return func() {
		mode = prev
	}
}

func loadMode() string {
	return normalizeMode(os.Getenv(ModeEnv))
}

func normalizeMode(value string) string {
	switch value {
	case ModeContent:
		return ModeContent
	default:
		return ModeMTime
	}
}
