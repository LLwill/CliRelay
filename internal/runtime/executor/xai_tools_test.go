package executor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestPrepareXAIResponsesBody_FlattensNamespaceTools(t *testing.T) {
	body := []byte(`{
		"model":"grok-4",
		"tools":[
			{"type":"namespace","name":"mcp__exa","tools":[{"type":"function","name":"web_search_exa","parameters":{"type":"object"}}]},
			{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}},
			{"type":"tool_search"},
			{"type":"image_generation"}
		],
		"tool_choice":{"type":"function","name":"web_search_exa","namespace":"mcp__exa"},
		"input":[{"role":"user","content":"hi"}]
	}`)
	out, refs := prepareXAIResponsesBody(body)

	if got := gjson.GetBytes(out, "tools.#").Int(); got != 2 {
		t.Fatalf("tools length = %d, want 2; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "mcp__exa__web_search_exa" {
		t.Fatalf("tools.0.name = %q, want mcp__exa__web_search_exa; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "tools.0.type").String(); got != "function" {
		t.Fatalf("tools.0.type = %q, want function; body=%s", got, string(out))
	}
	if gjson.GetBytes(out, `tools.#(type=="namespace")`).Exists() {
		t.Fatalf("namespace tools should be flattened away: %s", string(out))
	}
	if got := gjson.GetBytes(out, "tool_choice.name").String(); got != "mcp__exa__web_search_exa" {
		t.Fatalf("tool_choice.name = %q, want mcp__exa__web_search_exa; body=%s", got, string(out))
	}
	if gjson.GetBytes(out, "tool_choice.namespace").Exists() {
		t.Fatalf("tool_choice.namespace should be removed: %s", string(out))
	}
	if _, ok := refs["mcp__exa__web_search_exa"]; !ok {
		t.Fatalf("missing namespace ref map entry: %#v", refs)
	}
}

func TestNormalizeXAITools_QualifiesSameNamedNamespaceTools(t *testing.T) {
	body := []byte(`{
		"tools":[
			{"type":"namespace","name":"mcp__exa","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}]},
			{"type":"namespace","name":"mcp__docs","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}]}
		]
	}`)
	out := normalizeXAITools(body)

	tools := gjson.GetBytes(out, "tools").Array()
	if len(tools) != 2 {
		t.Fatalf("tools length = %d, want 2; body=%s", len(tools), string(out))
	}
	if got := tools[0].Get("name").String(); got != "mcp__exa__search" {
		t.Fatalf("tools.0.name = %q, want mcp__exa__search; body=%s", got, string(out))
	}
	if got := tools[1].Get("name").String(); got != "mcp__docs__search" {
		t.Fatalf("tools.1.name = %q, want mcp__docs__search; body=%s", got, string(out))
	}
}

func TestNormalizeXAITools_AdditionalToolsNamespace(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"mcp__exa","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}]}]},
			{"role":"user","content":"hello"}
		]
	}`)
	out := normalizeXAITools(body)

	tools := gjson.GetBytes(out, "input.0.tools").Array()
	if len(tools) != 1 {
		t.Fatalf("additional tools length = %d, want 1; body=%s", len(tools), string(out))
	}
	if got := tools[0].Get("name").String(); got != "mcp__exa__search" {
		t.Fatalf("additional tool name = %q, want mcp__exa__search; body=%s", got, string(out))
	}
	if got := tools[0].Get("type").String(); got != "function" {
		t.Fatalf("additional tool type = %q, want function; body=%s", got, string(out))
	}
}

func TestNormalizeXAITools_SimplifiesCodexAppAutomationUpdateSchema(t *testing.T) {
	params := `{"oneOf":[{"type":"object","properties":{"mode":{"type":"string"}}}],"$defs":{"a":{"type":"string"}},"x":"` + strings.Repeat("y", 1600) + `"}`
	body := []byte(`{"model":"grok-4.5","tools":[{"type":"namespace","name":"codex_app","tools":[{"type":"function","name":"automation_update","description":"sched","strict":true,"parameters":` + params + `}]},{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]}`)
	out := normalizeXAITools(body)

	foundAuto := false
	foundExec := false
	for _, tool := range gjson.GetBytes(out, "tools").Array() {
		switch tool.Get("name").String() {
		case "codex_app__automation_update":
			foundAuto = true
			paramsRaw := tool.Get("parameters").Raw
			if strings.Contains(paramsRaw, `"oneOf"`) || strings.Contains(paramsRaw, `"$defs"`) {
				t.Fatalf("automation_update parameters were not simplified: %s", paramsRaw)
			}
			if tool.Get("parameters.type").String() != "object" {
				t.Fatalf("automation_update parameters.type = %q, want object", tool.Get("parameters.type").String())
			}
			if tool.Get("parameters.additionalProperties").Type != gjson.True {
				t.Fatalf("automation_update parameters should allow additionalProperties: %s", paramsRaw)
			}
			if tool.Get("strict").Type != gjson.False {
				t.Fatalf("automation_update strict = %s, want false", tool.Get("strict").Raw)
			}
		case "exec_command":
			foundExec = true
		}
	}
	if !foundAuto {
		t.Fatalf("automation_update tool missing after normalize: %s", string(out))
	}
	if !foundExec {
		t.Fatalf("exec_command tool missing after normalize: %s", string(out))
	}
}

func TestNormalizeXAINamespaceToolChoice(t *testing.T) {
	body := []byte(`{
		"tools":[{"type":"namespace","name":"mcp__exa","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}]}],
		"tool_choice":{"type":"function","name":"search","namespace":"mcp__exa"}
	}`)
	out := normalizeXAITools(body)
	out = normalizeXAINamespaceToolChoice(out)

	if got := gjson.GetBytes(out, "tools.0.name").String(); got != "mcp__exa__search" {
		t.Fatalf("tools.0.name = %q, want mcp__exa__search; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "tool_choice.name").String(); got != "mcp__exa__search" {
		t.Fatalf("tool_choice.name = %q, want mcp__exa__search; body=%s", got, string(out))
	}
	if gjson.GetBytes(out, "tool_choice.namespace").Exists() {
		t.Fatalf("tool_choice.namespace should be removed for xAI upstream: %s", string(out))
	}
}

func TestNormalizeXAIInputNamespaceToolCalls(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call","name":"search","namespace":"mcp__exa","call_id":"call_1","arguments":"{}"}]}`)
	out := normalizeXAIInputNamespaceToolCalls(body)
	if got := gjson.GetBytes(out, "input.0.name").String(); got != "mcp__exa__search" {
		t.Fatalf("input.0.name = %q, want mcp__exa__search; body=%s", got, string(out))
	}
	if gjson.GetBytes(out, "input.0.namespace").Exists() {
		t.Fatalf("input.0.namespace should be removed: %s", string(out))
	}
}

func TestRestoreXAINamespaceToolCalls(t *testing.T) {
	refs := map[string]xaiNamespaceToolRef{
		"mcp__exa__web_search_exa": {namespace: "mcp__exa", name: "web_search_exa"},
	}
	event := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"mcp__exa__web_search_exa","call_id":"call_1","arguments":"{}"}}`)
	restoredEvent := restoreXAINamespaceToolCalls(event, refs)
	if got := gjson.GetBytes(restoredEvent, "item.name").String(); got != "web_search_exa" {
		t.Fatalf("item.name = %q, want child name; event=%s", got, string(restoredEvent))
	}
	if got := gjson.GetBytes(restoredEvent, "item.namespace").String(); got != "mcp__exa" {
		t.Fatalf("item.namespace = %q, want mcp__exa; event=%s", got, string(restoredEvent))
	}

	completed := []byte(`{"type":"response.completed","response":{"output":[{"type":"function_call","name":"mcp__exa__web_search_exa","call_id":"call_1","arguments":"{}"}]}}`)
	restoredCompleted := restoreXAINamespaceToolCalls(completed, refs)
	if got := gjson.GetBytes(restoredCompleted, "response.output.0.name").String(); got != "web_search_exa" {
		t.Fatalf("response.output.0.name = %q, want child name; event=%s", got, string(restoredCompleted))
	}
	if got := gjson.GetBytes(restoredCompleted, "response.output.0.namespace").String(); got != "mcp__exa" {
		t.Fatalf("response.output.0.namespace = %q, want mcp__exa; event=%s", got, string(restoredCompleted))
	}
}

func TestRestoreXAINamespaceToolCallsPreservesMalformedPayload(t *testing.T) {
	data := []byte(`{"item":{"type":"function_call","name":"mcp__exa__web_search_exa"`)
	refs := map[string]xaiNamespaceToolRef{
		"mcp__exa__web_search_exa": {namespace: "mcp__exa", name: "web_search_exa"},
	}
	if got := restoreXAINamespaceToolCalls(data, refs); !bytes.Equal(got, data) {
		t.Fatalf("malformed payload changed: got=%q want=%q", got, data)
	}
}

func TestNormalizeXAIToolChoiceForTools_DropsWhenToolsEmpty(t *testing.T) {
	body := []byte(`{"model":"grok-4","tools":[],"tool_choice":"auto","parallel_tool_calls":true,"input":"hi"}`)
	out := normalizeXAIToolChoiceForTools(body)
	if gjson.GetBytes(out, "tools").Exists() {
		t.Fatalf("empty tools should be removed: %s", string(out))
	}
	if gjson.GetBytes(out, "tool_choice").Exists() {
		t.Fatalf("tool_choice should be removed when tools empty: %s", string(out))
	}
	if gjson.GetBytes(out, "parallel_tool_calls").Exists() {
		t.Fatalf("parallel_tool_calls should be removed when tools empty: %s", string(out))
	}
}

func TestQualifyXAINamespaceToolNamePreservesQualifiedNames(t *testing.T) {
	if got := qualifyXAINamespaceToolName("mcp__exa", "mcp__exa__search"); got != "mcp__exa__search" {
		t.Fatalf("already-qualified name rewritten: %q", got)
	}
	if got := qualifyXAINamespaceToolName("mcp__exa", "search"); got != "mcp__exa__search" {
		t.Fatalf("short name not qualified: %q", got)
	}
	if got := qualifyXAINamespaceToolName("", "search"); got != "search" {
		t.Fatalf("empty namespace should keep short name: %q", got)
	}
}
