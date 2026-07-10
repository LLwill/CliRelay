package logging

import (
	"bytes"

	"github.com/gin-gonic/gin"
)

const apiExchangeProviderKey = "cliproxy.api_exchange_provider"

// APIExchangeProvider exposes a read-only snapshot of the upstream exchange.
// Implementations may capture chunks incrementally; callers request the full
// payload only at request finalization, avoiding repeated whole-body rebuilds.
type APIExchangeProvider interface {
	APIRequestSnapshot() []byte
	APIResponseSnapshot() []byte
}

// SetAPIExchangeProvider registers the per-request upstream exchange capture.
func SetAPIExchangeProvider(c *gin.Context, provider APIExchangeProvider) {
	if c == nil || provider == nil {
		return
	}
	c.Set(apiExchangeProviderKey, provider)
}

// CleanupAPIExchange releases optional per-request capture resources after all
// request logging and usage snapshots have been finalized.
func CleanupAPIExchange(c *gin.Context) {
	if c == nil {
		return
	}
	raw, ok := c.Get(apiExchangeProviderKey)
	if !ok {
		return
	}
	if closer, okCloser := raw.(interface{ Close() error }); okCloser && closer != nil {
		_ = closer.Close()
	}
	c.Set(apiExchangeProviderKey, nil)
}

// APIRequestSnapshot returns the finalized upstream request log.
func APIRequestSnapshot(c *gin.Context) []byte {
	return apiExchangeSnapshot(c, "API_REQUEST", true)
}

// APIResponseSnapshot returns the finalized upstream response log. If a
// handler also recorded a downstream/error payload under the legacy context
// key, it is appended once without mutating the incremental capture.
func APIResponseSnapshot(c *gin.Context) []byte {
	return apiExchangeSnapshot(c, "API_RESPONSE", false)
}

func apiExchangeSnapshot(c *gin.Context, legacyKey string, request bool) []byte {
	if c == nil {
		return nil
	}
	var captured []byte
	if raw, ok := c.Get(apiExchangeProviderKey); ok {
		if provider, okProvider := raw.(APIExchangeProvider); okProvider && provider != nil {
			if request {
				captured = provider.APIRequestSnapshot()
			} else {
				captured = provider.APIResponseSnapshot()
			}
		}
	}
	legacy, _ := c.Get(legacyKey)
	legacyBytes, _ := legacy.([]byte)
	return mergeAPIExchangeSnapshot(captured, legacyBytes)
}

func mergeAPIExchangeSnapshot(captured, legacy []byte) []byte {
	captured = bytes.TrimSpace(captured)
	legacy = bytes.TrimSpace(legacy)
	switch {
	case len(captured) == 0 && len(legacy) == 0:
		return nil
	case len(captured) == 0:
		return legacy
	case len(legacy) == 0 || bytes.Contains(captured, legacy):
		return captured
	default:
		out := make([]byte, 0, len(captured)+len(legacy)+1)
		out = append(out, captured...)
		out = append(out, '\n')
		out = append(out, legacy...)
		return out
	}
}
