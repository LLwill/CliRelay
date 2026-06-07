package requestctx

// ContextKey is a strongly-typed context key to avoid collisions with other
// packages and satisfy static analysis checks (SA1029).
type ContextKey string

const (
	// ContextKeyAlt carries an executor "alt" mode hint (e.g. "responses/compact").
	ContextKeyAlt ContextKey = "alt"

	// ContextKeyGin carries a *gin.Context for request-scoped logging and helpers.
	// It is intentionally stored as an opaque value here to avoid import cycles.
	ContextKeyGin ContextKey = "gin"

	// ContextKeyRoundTripper carries an optional http.RoundTripper override used
	// by proxy-aware HTTP clients.
	ContextKeyRoundTripper ContextKey = "cliproxy.roundtripper"

	// ContextKeyAPIKey carries a synthetic API key label for non-HTTP execution
	// paths that still need request-log attribution.
	ContextKeyAPIKey ContextKey = "cliproxy.api_key"

	// ContextKeyImageGenerationPhaseHook carries an optional callback that
	// receives backend image-generation phase updates.
	ContextKeyImageGenerationPhaseHook ContextKey = "cliproxy.image_generation.phase_hook"
)

const (
	// GinKeyFirstResponseAt stores the timestamp of the first downstream response
	// chunk written to the client, used to derive first-token latency.
	GinKeyFirstResponseAt = "cliproxy.first_response_at"
)
