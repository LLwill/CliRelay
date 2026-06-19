package api

import (
	"testing"

	internalrouting "github.com/router-for-me/CLIProxyAPI/v6/internal/routing"
)

func TestFilterCodexModelsForCcSwitchRouteReturnsRequestModels(t *testing.T) {
	models := []map[string]interface{}{
		{"id": "deepseek-chat", "object": "model", "owned_by": "deepseek"},
		{"id": "kimi-k2", "object": "model", "owned_by": "moonshot"},
		{"id": "gpt-5.5", "object": "model", "owned_by": "openai"},
	}
	route := &internalrouting.PathRouteContext{
		CcSwitch: &internalrouting.CcSwitchRouteContext{
			ClientType:   "codex",
			DefaultModel: "deepseek-v4-flash",
			ModelMappings: []internalrouting.CcSwitchModelMapping{
				{RequestModel: "deepseek-v4-flash", TargetModel: "deepseek-chat"},
				{RequestModel: "deepseek-v4-pro", TargetModel: "deepseek-chat"},
			},
		},
	}

	filtered := filterModelsForCcSwitchRoute(models, route)
	got := modelIDs(filtered)
	want := []string{"deepseek-v4-flash", "deepseek-v4-pro"}
	if !sameStrings(got, want) {
		t.Fatalf("model ids = %#v, want %#v", got, want)
	}
	if filtered[0]["owned_by"] != "deepseek" {
		t.Fatalf("owned_by = %v, want deepseek", filtered[0]["owned_by"])
	}
}

func TestCcSwitchRequestModelAllowedForTarget(t *testing.T) {
	route := &internalrouting.PathRouteContext{
		CcSwitch: &internalrouting.CcSwitchRouteContext{
			ModelMappings: []internalrouting.CcSwitchModelMapping{
				{RequestModel: "deepseek-v4-flash", TargetModel: "deepseek-chat"},
			},
		},
	}

	if !ccSwitchRequestModelAllowedForTarget("deepseek-chat", route, map[string]struct{}{"deepseek-v4-flash": {}}) {
		t.Fatal("request model alias was not allowed for target")
	}
	if ccSwitchRequestModelAllowedForTarget("kimi-k2", route, map[string]struct{}{"deepseek-v4-flash": {}}) {
		t.Fatal("unmapped target was allowed")
	}
}

func modelIDs(models []map[string]interface{}) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if id, ok := model["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
