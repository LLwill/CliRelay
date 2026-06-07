package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func SystemPromptMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		metadataVal, exists := c.Get("accessMetadata")
		if !exists {
			c.Next()
			return
		}
		metadata, ok := metadataVal.(map[string]string)
		if !ok {
			c.Next()
			return
		}
		systemPrompt, exists := metadata["system-prompt"]
		if !exists || strings.TrimSpace(systemPrompt) == "" {
			c.Next()
			return
		}

		bodyBytes, err := bodyutil.ReadRequestBody(c, bodyutil.DefaultRequestBodyLimit)
		if err != nil {
			if bodyutil.IsTooLarge(err) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.Next()
			return
		}

		var newBody []byte
		if gjson.GetBytes(bodyBytes, "messages").Exists() && gjson.GetBytes(bodyBytes, "messages").IsArray() {
			sysMsg := map[string]interface{}{
				"role":    "system",
				"content": systemPrompt,
			}
			var body map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				c.Next()
				return
			}
			messages, _ := body["messages"].([]interface{})
			newMessages := make([]interface{}, 0, len(messages)+1)
			newMessages = append(newMessages, sysMsg)
			newMessages = append(newMessages, messages...)
			body["messages"] = newMessages
			newBody, err = json.Marshal(body)
			if err != nil {
				c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				c.Next()
				return
			}
			log.Debugf("[SystemPrompt] injected into messages (count: %d→%d)", len(messages), len(newMessages))
		} else if gjson.GetBytes(bodyBytes, "input").Exists() {
			existing := strings.TrimSpace(gjson.GetBytes(bodyBytes, "instructions").String())
			combined := systemPrompt
			if existing != "" {
				combined = systemPrompt + "\n\n" + existing
			}
			newBody, _ = sjson.SetBytes(bodyBytes, "instructions", combined)
			if newBody == nil {
				c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				c.Next()
				return
			}
			log.Debugf("[SystemPrompt] injected into instructions (Responses API)")
		} else {
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			c.Next()
			return
		}

		c.Request.Body = io.NopCloser(bytes.NewReader(newBody))
		c.Request.ContentLength = int64(len(newBody))
		c.Next()
	}
}
