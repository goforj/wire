package wire

import "context"

type bypassPackageCacheKey struct{}

func withBypassPackageCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, bypassPackageCacheKey{}, true)
}

func bypassPackageCache(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(bypassPackageCacheKey{}).(bool)
	return v
}
