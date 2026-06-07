package executor

import "github.com/tidwall/sjson"

// setJSONField sets a top-level JSON field on a byte slice payload via sjson.
func setJSONField(body []byte, key, value string) []byte {
	if key == "" {
		return body
	}
	updated, err := sjson.SetBytes(body, key, value)
	if err != nil {
		return body
	}
	return updated
}

// deleteJSONField removes a top-level key if present (best-effort) via sjson.
func deleteJSONField(body []byte, key string) []byte {
	if key == "" || len(body) == 0 {
		return body
	}
	updated, err := sjson.DeleteBytes(body, key)
	if err != nil {
		return body
	}
	return updated
}
