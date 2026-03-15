package loader

import (
	"context"
	"time"
)

type timingLogger func(string, time.Duration)

type timingKey struct{}

func WithTiming(ctx context.Context, logf func(string, time.Duration)) context.Context {
	if logf == nil {
		return ctx
	}
	return context.WithValue(ctx, timingKey{}, timingLogger(logf))
}

func timing(ctx context.Context) timingLogger {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(timingKey{}); v != nil {
		if t, ok := v.(timingLogger); ok {
			return t
		}
	}
	return nil
}

func logTiming(ctx context.Context, label string, start time.Time) {
	if t := timing(ctx); t != nil {
		t(label, time.Since(start))
	}
}

func logDuration(ctx context.Context, label string, d time.Duration) {
	if t := timing(ctx); t != nil {
		t(label, d)
	}
}
