package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const visionPreprocessTimeout = 30 * time.Second

// opencodeGoVisionPreprocessImage sends a base64-encoded image to the configured
// vision model via OpenCode and returns a concise text description. When the API
// call fails we return a generic placeholder so the calling code can degrade
// gracefully.
func opencodeGoVisionPreprocessImage(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, apiKey, visionModel, imageData, imageType string) (string, error) {
	prompt := "Describe what you see in this image concisely in one or two sentences. Focus on visual details relevant to a coding assistant."

	body := buildVisionRequestBody(visionModel, prompt, imageData, imageType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opencodeGoBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build vision request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	httpClient := newProxyAwareHTTPClient(ctx, cfg, auth, 0)
	httpResp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", fmt.Errorf("read vision response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", fmt.Errorf("vision API status %d: %s", httpResp.StatusCode, string(respBody))
	}

	description := gjson.GetBytes(respBody, "choices.0.message.content").String()
	if description == "" {
		return "", fmt.Errorf("empty vision response")
	}

	return strings.TrimSpace(description), nil
}

func buildVisionRequestBody(model, prompt, imageData, imageType string) []byte {
	var mimeType string
	switch strings.ToLower(imageType) {
	case "png":
		mimeType = "image/png"
	case "jpg", "jpeg":
		mimeType = "image/jpeg"
	case "gif":
		mimeType = "image/gif"
	case "webp":
		mimeType = "image/webp"
	default:
		mimeType = "image/png"
	}

	dataURL := "data:" + mimeType + ";base64," + imageData

	body := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": prompt},
					{"type": "image_url", "image_url": map[string]any{"url": dataURL}},
				},
			},
		},
		"max_tokens": 512,
	}

	out, _ := json.Marshal(body)
	return out
}

// opencodeGoExtractImageData extracts base64 image data from a content part.
func opencodeGoExtractImageData(part map[string]any) (data, imgType string) {
	// Format 1: {type:"image_url", image_url:{url:"data:image/png;base64,..."}}
	if imageURL, ok := part["image_url"].(map[string]any); ok {
		if url, ok := imageURL["url"].(string); ok {
			return parseDataURL(url)
		}
	}
	// Format 2: {type:"input_image", image_url:"data:image/png;base64,..."}
	if url, ok := part["image_url"].(string); ok {
		return parseDataURL(url)
	}
	// Format 3: direct base64 string in "source" field
	if source, ok := part["source"].(map[string]any); ok {
		if data, ok := source["data"].(string); ok {
			mediaType, _ := source["media_type"].(string)
			return data, mediaType
		}
	}
	return "", ""
}

func parseDataURL(url string) (data, mediaType string) {
	// data:image/png;base64,iVBOR...
	if !strings.HasPrefix(url, "data:") {
		return url, "" // Not a data URL, return as-is
	}
	parts := strings.SplitN(url, ",", 2)
	if len(parts) != 2 {
		return url, ""
	}
	header := parts[0] // data:image/png;base64
	data = parts[1]

	// Extract media type from header
	header = strings.TrimPrefix(header, "data:")
	if semi := strings.Index(header, ";"); semi >= 0 {
		mediaType = header[:semi]
	} else {
		mediaType = header
	}
	return data, mediaType
}

// opencodeGoHasCurrentImage checks if the very last message in the payload
// (Chat Completions messages array or Responses API input array) is a user
// message containing an image part.
func opencodeGoHasCurrentImage(payload []byte) bool {
	// Check Chat Completions format first
	msgs := gjson.GetBytes(payload, "messages")
	if msgs.Exists() && msgs.IsArray() && len(msgs.Array()) > 0 {
		last := msgs.Array()[len(msgs.Array())-1]
		if last.Get("role").String() != "user" {
			return false
		}
		return contentHasImage(last)
	}

	// Check Responses API format
	input := gjson.GetBytes(payload, "input")
	if input.Exists() && input.IsArray() && len(input.Array()) > 0 {
		last := input.Array()[len(input.Array())-1]
		if last.Get("role").String() != "user" {
			return false
		}
		return contentHasImage(last)
	}
	return false
}

func contentHasImage(item gjson.Result) bool {
	content := item.Get("content")
	if !content.Exists() {
		return false
	}
	if content.IsArray() {
		for _, part := range content.Array() {
			partType := part.Get("type").String()
			if partType == "image_url" || partType == "input_image" || partType == "image" {
				return true
			}
			if part.Get("image_url").Exists() {
				return true
			}
		}
	}
	// Plain string content with data URL
	text := content.String()
	if strings.HasPrefix(text, "data:image") {
		return true
	}
	return false
}

// opencodeGoPreprocessVision replaces images in the current user message with
// text descriptions from the configured vision model (vision_fallback_model).
// The model in the payload is NOT changed — requests always stay on the
// original model.
//
// Returns the modified payload and a boolean indicating whether any replacement
// was performed.
func opencodeGoPreprocessVision(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, apiKey, visionModel string, payload []byte) ([]byte, bool) {
	if visionModel == "" || len(payload) == 0 || !gjson.ValidBytes(payload) || apiKey == "" {
		return payload, false
	}

	// Wrap context with timeout
	visionCtx, cancel := context.WithTimeout(ctx, visionPreprocessTimeout)
	defer cancel()

	modified := false
	var procErr error

	// Helper: process a single content array and replace images
	processContentArray := func(msgIdx int, content gjson.Result) []byte {
		parts := content.Array()
		for pIdx := len(parts) - 1; pIdx >= 0; pIdx-- {
			part := parts[pIdx]
			partType := part.Get("type").String()
			if partType != "image_url" && partType != "input_image" && partType != "image" && !part.Get("image_url").Exists() {
				continue
			}

			// Extract image data
			var partMap map[string]any
			if err := json.Unmarshal([]byte(part.Raw), &partMap); err != nil {
				procErr = err
				continue
			}
			imgData, imgType := opencodeGoExtractImageData(partMap)
			if imgData == "" {
				continue
			}

			// Call qwen to describe the image
			description, err := opencodeGoVisionPreprocessImage(visionCtx, cfg, auth, apiKey, visionModel, imgData, imgType)
			if err != nil {
				procErr = err
				description = "[Image description unavailable]"
			}

			// Replace the image part with a text part containing the description
			contentPath := fmt.Sprintf("messages.%d.content.%d", msgIdx, pIdx)
			payload, _ = sjson.SetBytes(payload, contentPath+".type", "text")
			payload, _ = sjson.SetBytes(payload, contentPath+".text", "[Image: "+description+"]")
			payload, _ = sjson.DeleteBytes(payload, contentPath+".image_url")
			payload, _ = sjson.DeleteBytes(payload, contentPath+".detail")
			modified = true
		}
		return payload
	}

	// Process Chat Completions format (messages array)
	messages := gjson.GetBytes(payload, "messages")
	if messages.Exists() && messages.IsArray() {
		items := messages.Array()
		for i := len(items) - 1; i >= 0; i-- {
			item := items[i]
			if item.Get("role").String() != "user" {
				continue
			}
			content := item.Get("content")
			if content.Exists() && content.IsArray() {
				payload = processContentArray(i, content)
			}
		}
	}

	// Process Responses API format (input array)
	input := gjson.GetBytes(payload, "input")
	if input.Exists() && input.IsArray() {
		items := input.Array()
		for i := len(items) - 1; i >= 0; i-- {
			item := items[i]
			if item.Get("role").String() != "user" {
				continue
			}
			content := item.Get("content")
			if content.Exists() && content.IsArray() {
				parts := content.Array()
				for pIdx := len(parts) - 1; pIdx >= 0; pIdx-- {
					part := parts[pIdx]
					partType := part.Get("type").String()
					if partType != "input_image" && partType != "image_url" && partType != "image" && !part.Get("image_url").Exists() {
						continue
					}
					var partMap map[string]any
					if err := json.Unmarshal([]byte(part.Raw), &partMap); err != nil {
						procErr = err
						continue
					}
					imgData, imgType := opencodeGoExtractImageData(partMap)
					if imgData == "" {
						continue
					}
					description, err := opencodeGoVisionPreprocessImage(visionCtx, cfg, auth, apiKey, visionModel, imgData, imgType)
					if err != nil {
						procErr = err
						description = "[Image description unavailable]"
					}
					contentPath := fmt.Sprintf("input.%d.content.%d", i, pIdx)
					payload, _ = sjson.SetBytes(payload, contentPath+".type", "input_text")
					payload, _ = sjson.SetBytes(payload, contentPath+".text", "[Image: "+description+"]")
					payload, _ = sjson.DeleteBytes(payload, contentPath+".image_url")
					payload, _ = sjson.DeleteBytes(payload, contentPath+".detail")
					modified = true
				}
			}
		}
	}

	if procErr != nil {
		// Log the error but still return the (partially) modified payload
		log.Warnf("opencode go vision preprocess: %v", procErr)
	}

	return payload, modified
}

// opencodeGoAPIKey extracts the OpenCode API key from an auth object.
