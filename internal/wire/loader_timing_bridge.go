package wire

import (
	"context"
	"time"

	"github.com/goforj/wire/internal/loader"
)

func withLoaderTiming(ctx context.Context) context.Context {
	if t := timing(ctx); t != nil {
		return loader.WithTiming(ctx, func(label string, d time.Duration) {
			t(label, d)
		})
	}
	return ctx
}
