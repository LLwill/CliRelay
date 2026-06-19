package openai

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestRewriteCcSwitchOpenAIRequestModelMapsCodexRequestModel(t *testing.T) {
	rawJSON := []byte(`{"model":"deepseek-v4-flash","input":"ok"}`)
	c := &gin.Context{}
	c.Set(ccSwitchOpenAIModelMappingContextKey, map[string]string{
		"deepseek-v4-flash": "deepseek-chat",
	})

	rewritten, modelName, mapped := rewriteCcSwitchOpenAIRequestModel(rawJSON, c)
	if !mapped {
		t.Fatal("mapped = false, want true")
	}
	if modelName != "deepseek-chat" {
		t.Fatalf("modelName = %q, want deepseek-chat", modelName)
	}
	if got := gjson.GetBytes(rewritten, "model").String(); got != "deepseek-chat" {
		t.Fatalf("rewritten model = %q, want deepseek-chat", got)
	}
	if got := gjson.GetBytes(rawJSON, "model").String(); got != "deepseek-v4-flash" {
		t.Fatalf("input rawJSON mutated, model = %q", got)
	}
}

func TestRewriteCcSwitchOpenAIRequestModelIgnoresClaudeRoute(t *testing.T) {
	rawJSON := []byte(`{"model":"deepseek-v4-flash","input":"ok"}`)

	rewritten, modelName, mapped := rewriteCcSwitchOpenAIRequestModel(rawJSON, &gin.Context{})
	if mapped {
		t.Fatal("mapped = true, want false")
	}
	if modelName != "deepseek-v4-flash" {
		t.Fatalf("modelName = %q, want original model", modelName)
	}
	if string(rewritten) != string(rawJSON) {
		t.Fatalf("rewritten JSON changed: %s", rewritten)
	}
}
