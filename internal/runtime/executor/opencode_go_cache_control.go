package executor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type opencodeGoCacheControlPart struct {
	role  string
	value string
}

func opencodeGoPreserveClaudeCacheControl(translated, source []byte) []byte {
	controls := opencodeGoCollectClaudeCacheControls(source)
	if len(controls) == 0 || !gjson.ValidBytes(translated) {
		return translated
	}

	out := translated
	controlIndex := 0
	messages := gjson.GetBytes(out, "messages")
	if !messages.IsArray() {
		return out
	}
	messages.ForEach(func(messageIndex, message gjson.Result) bool {
		if controlIndex >= len(controls) {
			return false
		}
		role := message.Get("role").String()
		content := message.Get("content")
		if !content.IsArray() {
			if role == "tool" && role == controls[controlIndex].role && content.Type == gjson.String && content.String() != "" {
				path := fmt.Sprintf("messages.%d.content", messageIndex.Int())
				part, err := opencodeGoToolContentWithCacheControl(content.String(), controls[controlIndex].value)
				if err == nil {
					updated, err := sjson.SetRawBytes(out, path, part)
					if err == nil {
						out = updated
						controlIndex++
					}
				}
			}
			return true
		}
		content.ForEach(func(contentIndex, part gjson.Result) bool {
			if controlIndex >= len(controls) {
				return false
			}
			if role != controls[controlIndex].role {
				return true
			}
			if part.Get("cache_control").Exists() || !opencodeGoChatContentPartSupportsCacheControl(part) {
				return true
			}
			path := fmt.Sprintf("messages.%d.content.%d.cache_control", messageIndex.Int(), contentIndex.Int())
			updated, err := sjson.SetRawBytes(out, path, []byte(controls[controlIndex].value))
			if err == nil {
				out = updated
				controlIndex++
			}
			return true
		})
		return true
	})
	return out
}

func opencodeGoCollectClaudeCacheControls(source []byte) []opencodeGoCacheControlPart {
	if !gjson.ValidBytes(source) {
		return nil
	}
	controls := make([]opencodeGoCacheControlPart, 0, 4)
	system := gjson.GetBytes(source, "system")
	if system.IsArray() {
		system.ForEach(func(_, part gjson.Result) bool {
			opencodeGoAppendClaudeCacheControl(&controls, "system", part, nil)
			return true
		})
	}
	messages := gjson.GetBytes(source, "messages")
	if messages.IsArray() {
		validToolUseIDs := map[string]bool{}
		messages.ForEach(func(_, message gjson.Result) bool {
			role := message.Get("role").String()
			content := message.Get("content")
			if !content.IsArray() {
				return true
			}
			content.ForEach(func(_, part gjson.Result) bool {
				if role == "assistant" && part.Get("type").String() == "tool_use" && strings.TrimSpace(part.Get("name").String()) != "" {
					if id := part.Get("id").String(); id != "" {
						validToolUseIDs[id] = true
					}
				}
				opencodeGoAppendClaudeCacheControl(&controls, role, part, validToolUseIDs)
				return true
			})
			return true
		})
	}
	return controls
}

func opencodeGoAppendClaudeCacheControl(controls *[]opencodeGoCacheControlPart, role string, part gjson.Result, validToolUseIDs map[string]bool) {
	cacheControl := part.Get("cache_control")
	targetRole, ok := opencodeGoClaudePartMapsToChatRole(role, part, validToolUseIDs)
	if !ok || !cacheControl.Exists() {
		return
	}
	*controls = append(*controls, opencodeGoCacheControlPart{
		role:  targetRole,
		value: cacheControl.Raw,
	})
}

func opencodeGoClaudePartMapsToChatRole(role string, part gjson.Result, validToolUseIDs map[string]bool) (string, bool) {
	if role == "" {
		return "", false
	}
	switch part.Get("type").String() {
	case "text":
		return role, part.Get("text").String() != ""
	case "image":
		return role, part.Get("source").Exists() || part.Get("url").String() != ""
	case "tool_result":
		toolUseID := part.Get("tool_use_id").String()
		return "tool", toolUseID != "" && validToolUseIDs[toolUseID] && part.Get("content").Exists()
	default:
		return "", false
	}
}

func opencodeGoChatContentPartSupportsCacheControl(part gjson.Result) bool {
	switch part.Get("type").String() {
	case "text", "image_url":
		return true
	default:
		return false
	}
}

func opencodeGoToolContentWithCacheControl(text, cacheControl string) ([]byte, error) {
	part := map[string]any{
		"type":          "text",
		"text":          text,
		"cache_control": json.RawMessage(cacheControl),
	}
	return json.Marshal([]map[string]any{part})
}
