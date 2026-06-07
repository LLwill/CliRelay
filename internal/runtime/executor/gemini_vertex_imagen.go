package executor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// isImagenModel checks if the model name is an Imagen image generation model.
// Imagen models use the :predict action instead of :generateContent.
func isImagenModel(model string) bool {
	lowerModel := strings.ToLower(model)
	return strings.Contains(lowerModel, "imagen")
}

// getVertexAction returns the appropriate action for the given model.
// Imagen models use "predict", while Gemini models use "generateContent".
func getVertexAction(model string, isStream bool) string {
	if isImagenModel(model) {
		return "predict"
	}
	if isStream {
		return "streamGenerateContent"
	}
	return "generateContent"
}

// convertImagenToGeminiResponse converts Imagen API response to Gemini format
// so it can be processed by the standard translation pipeline.
// This ensures Imagen models return responses in the same format as gemini-3-pro-image-preview.
func convertImagenToGeminiResponse(data []byte, model string) []byte {
	predictions := gjson.GetBytes(data, "predictions")
	if !predictions.Exists() || !predictions.IsArray() {
		return data
	}

	// Build Gemini-compatible response with inlineData
	parts := make([]map[string]any, 0)
	for _, pred := range predictions.Array() {
		imageData := pred.Get("bytesBase64Encoded").String()
		mimeType := pred.Get("mimeType").String()
		if mimeType == "" {
			mimeType = "image/png"
		}
		if imageData != "" {
			parts = append(parts, map[string]any{
				"inlineData": map[string]any{
					"mimeType": mimeType,
					"data":     imageData,
				},
			})
		}
	}

	// Generate unique response ID using timestamp
	responseID := fmt.Sprintf("imagen-%d", time.Now().UnixNano())

	response := map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{
				"parts": parts,
				"role":  "model",
			},
			"finishReason": "STOP",
		}},
		"responseId":   responseID,
		"modelVersion": model,
		// Imagen API doesn't return token counts, set to 0 for tracking purposes
		"usageMetadata": map[string]any{
			"promptTokenCount":     0,
			"candidatesTokenCount": 0,
			"totalTokenCount":      0,
		},
	}

	result, err := json.Marshal(response)
	if err != nil {
		return data
	}
	return result
}

// convertToImagenRequest converts a Gemini-style request to Imagen API format.
// Imagen API uses a different structure: instances[].prompt instead of contents[].
func convertToImagenRequest(payload []byte) ([]byte, error) {
	// Extract prompt from Gemini-style contents
	prompt := ""

	// Try to get prompt from contents[0].parts[0].text
	contentsText := gjson.GetBytes(payload, "contents.0.parts.0.text")
	if contentsText.Exists() {
		prompt = contentsText.String()
	}

	// If no contents, try messages format (OpenAI-compatible)
	if prompt == "" {
		messagesText := gjson.GetBytes(payload, "messages.#.content")
		if messagesText.Exists() && messagesText.IsArray() {
			for _, msg := range messagesText.Array() {
				if msg.String() != "" {
					prompt = msg.String()
					break
				}
			}
		}
	}

	// If still no prompt, try direct prompt field
	if prompt == "" {
		directPrompt := gjson.GetBytes(payload, "prompt")
		if directPrompt.Exists() {
			prompt = directPrompt.String()
		}
	}

	if prompt == "" {
		return nil, fmt.Errorf("imagen: no prompt found in request")
	}

	// Build Imagen API request
	imagenReq := map[string]any{
		"instances": []map[string]any{
			{
				"prompt": prompt,
			},
		},
		"parameters": map[string]any{
			"sampleCount": 1,
		},
	}

	// Extract optional parameters
	if aspectRatio := gjson.GetBytes(payload, "aspectRatio"); aspectRatio.Exists() {
		imagenReq["parameters"].(map[string]any)["aspectRatio"] = aspectRatio.String()
	}
	if sampleCount := gjson.GetBytes(payload, "sampleCount"); sampleCount.Exists() {
		imagenReq["parameters"].(map[string]any)["sampleCount"] = int(sampleCount.Int())
	}
	if negativePrompt := gjson.GetBytes(payload, "negativePrompt"); negativePrompt.Exists() {
		imagenReq["instances"].([]map[string]any)[0]["negativePrompt"] = negativePrompt.String()
	}

	return json.Marshal(imagenReq)
}
