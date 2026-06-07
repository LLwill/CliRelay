package executor

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// applyGeminiCLIHeaders sets required headers for the Gemini CLI upstream.
func applyGeminiCLIHeaders(r *http.Request) {
	var ginHeaders http.Header
	if ginCtx, ok := r.Context().Value(util.ContextKeyGin).(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
		ginHeaders = ginCtx.Request.Header
	}

	misc.EnsureHeader(r.Header, ginHeaders, "User-Agent", "google-api-nodejs-client/9.15.1")
	misc.EnsureHeader(r.Header, ginHeaders, "X-Goog-Api-Client", "gl-node/22.17.0")
	misc.EnsureHeader(r.Header, ginHeaders, "Client-Metadata", geminiCLIClientMetadata())
}

// geminiCLIClientMetadata returns a compact metadata string required by upstream.
func geminiCLIClientMetadata() string {
	// Keep parity with CLI client defaults
	return "ideType=IDE_UNSPECIFIED,platform=PLATFORM_UNSPECIFIED,pluginType=GEMINI"
}

// cliPreviewFallbackOrder returns preview model candidates for a base model.
func cliPreviewFallbackOrder(model string) []string {
	switch model {
	case "gemini-2.5-pro":
		return []string{
			// "gemini-2.5-pro-preview-05-06",
			// "gemini-2.5-pro-preview-06-05",
		}
	case "gemini-2.5-flash":
		return []string{
			// "gemini-2.5-flash-preview-04-17",
			// "gemini-2.5-flash-preview-05-20",
		}
	case "gemini-2.5-flash-lite":
		return []string{
			// "gemini-2.5-flash-lite-preview-06-17",
		}
	default:
		return nil
	}
}

func fixGeminiCLIImageAspectRatio(modelName string, rawJSON []byte) []byte {
	if modelName == "gemini-2.5-flash-image-preview" {
		aspectRatioResult := gjson.GetBytes(rawJSON, "request.generationConfig.imageConfig.aspectRatio")
		if aspectRatioResult.Exists() {
			contents := gjson.GetBytes(rawJSON, "request.contents")
			contentArray := contents.Array()
			if len(contentArray) > 0 {
				hasInlineData := false
			loopContent:
				for i := 0; i < len(contentArray); i++ {
					parts := contentArray[i].Get("parts").Array()
					for j := 0; j < len(parts); j++ {
						if parts[j].Get("inlineData").Exists() {
							hasInlineData = true
							break loopContent
						}
					}
				}

				if !hasInlineData {
					emptyImageBase64ed, _ := util.CreateWhiteImageBase64(aspectRatioResult.String())
					emptyImagePart := `{"inlineData":{"mime_type":"image/png","data":""}}`
					emptyImagePart, _ = sjson.Set(emptyImagePart, "inlineData.data", emptyImageBase64ed)
					newPartsJSON := `[]`
					newPartsJSON, _ = sjson.SetRaw(newPartsJSON, "-1", `{"text": "Based on the following requirements, create an image within the uploaded picture. The new content *MUST* completely cover the entire area of the original picture, maintaining its exact proportions, and *NO* blank areas should appear."}`)
					newPartsJSON, _ = sjson.SetRaw(newPartsJSON, "-1", emptyImagePart)

					parts := contentArray[0].Get("parts").Array()
					for j := 0; j < len(parts); j++ {
						newPartsJSON, _ = sjson.SetRaw(newPartsJSON, "-1", parts[j].Raw)
					}

					rawJSON, _ = sjson.SetRawBytes(rawJSON, "request.contents.0.parts", []byte(newPartsJSON))
					rawJSON, _ = sjson.SetRawBytes(rawJSON, "request.generationConfig.responseModalities", []byte(`["IMAGE", "TEXT"]`))
				}
			}
			rawJSON, _ = sjson.DeleteBytes(rawJSON, "request.generationConfig.imageConfig")
		}
	}
	return rawJSON
}
