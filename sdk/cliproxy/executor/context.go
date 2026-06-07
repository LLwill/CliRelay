package executor

import (
	"context"
	"net/http"
)

type downstreamWebsocketContextKey struct{}
type roundTripperContextKey struct{}

// WithDownstreamWebsocket marks the current request as coming from a downstream websocket connection.
func WithDownstreamWebsocket(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, downstreamWebsocketContextKey{}, true)
}

// DownstreamWebsocket reports whether the current request originates from a downstream websocket connection.
func DownstreamWebsocket(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw := ctx.Value(downstreamWebsocketContextKey{})
	enabled, ok := raw.(bool)
	return ok && enabled
}

// WithRoundTripper returns a child context carrying an optional HTTP transport override.
func WithRoundTripper(ctx context.Context, rt http.RoundTripper) context.Context {
	if rt == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, roundTripperContextKey{}, rt)
}

// RoundTripperFromContext extracts a previously attached transport override.
func RoundTripperFromContext(ctx context.Context) http.RoundTripper {
	if ctx == nil {
		return nil
	}
	raw := ctx.Value(roundTripperContextKey{})
	rt, _ := raw.(http.RoundTripper)
	return rt
}
