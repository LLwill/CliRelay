package responses

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponsesRequestToOpenAIChatCompletionsFlattensNamespaceAndCustomTools(t *testing.T) {
	request := []byte(`{
		"model":"gpt-test",
		"input":"hello",
		"tools":[
			{"type":"namespace","name":"functions","tools":[
				{"type":"function","name":"exec","description":"run","parameters":{"type":"object"}},
				{"type":"custom","name":"patch","description":"apply patch"}
			]},
			{"type":"web_search"}
		],
		"tool_choice":"auto"
	}`)

	got := ConvertOpenAIResponsesRequestToOpenAIChatCompletions("gpt-test", request, false)
	if count := gjson.GetBytes(got, "tools.#").Int(); count != 2 {
		t.Fatalf("tools count = %d, want 2; payload=%s", count, got)
	}
	if name := gjson.GetBytes(got, "tools.0.function.name").String(); name != "functions__exec" {
		t.Fatalf("namespace function name = %q", name)
	}
	if name := gjson.GetBytes(got, "tools.1.function.name").String(); name != "functions__patch" {
		t.Fatalf("namespace custom name = %q", name)
	}
	if inputType := gjson.GetBytes(got, "tools.1.function.parameters.properties.input.type").String(); inputType != "string" {
		t.Fatalf("custom input type = %q", inputType)
	}
	if choice := gjson.GetBytes(got, "tool_choice").String(); choice != "auto" {
		t.Fatalf("tool_choice = %q", choice)
	}
}

func TestConvertOpenAIResponsesRequestToOpenAIChatCompletionsDropsChoiceWithoutSupportedTools(t *testing.T) {
	request := []byte(`{"model":"gpt-test","input":"hello","tools":[{"type":"web_search"}],"tool_choice":"auto"}`)
	got := ConvertOpenAIResponsesRequestToOpenAIChatCompletions("gpt-test", request, false)
	if gjson.GetBytes(got, "tools").Exists() {
		t.Fatalf("unexpected tools: %s", got)
	}
	if gjson.GetBytes(got, "tool_choice").Exists() {
		t.Fatalf("tool_choice must be absent when tools are absent: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToOpenAIChatCompletionsIncludesAdditionalTools(t *testing.T) {
	request := []byte(`{
		"model":"gpt-test",
		"input":[
			{"type":"additional_tools","tools":[{"type":"function","name":"late_tool","parameters":{"type":"object"}}]},
			{"role":"user","content":"hello"}
		],
		"tool_choice":"auto"
	}`)
	got := ConvertOpenAIResponsesRequestToOpenAIChatCompletions("gpt-test", request, false)
	if name := gjson.GetBytes(got, "tools.0.function.name").String(); name != "late_tool" {
		t.Fatalf("additional tool name = %q; payload=%s", name, got)
	}
}

func TestConvertOpenAIChatCompletionsResponseRestoresNamespaceTool(t *testing.T) {
	request := []byte(`{"tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}]}`)
	response := []byte(`{"id":"chat_1","created":1,"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"functions__exec","arguments":"{}"}}]}}]}`)

	got := ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(context.Background(), "gpt-test", request, nil, response, nil)
	if name := gjson.Get(got, "output.0.name").String(); name != "exec" {
		t.Fatalf("restored name = %q; payload=%s", name, got)
	}
	if namespace := gjson.Get(got, "output.0.namespace").String(); namespace != "functions" {
		t.Fatalf("restored namespace = %q; payload=%s", namespace, got)
	}
	if itemType := gjson.Get(got, "output.0.type").String(); itemType != "function_call" {
		t.Fatalf("restored type = %q", itemType)
	}
}

func TestConvertOpenAIChatCompletionsResponseRestoresCustomTool(t *testing.T) {
	request := []byte(`{"tools":[{"type":"custom","name":"apply_patch","description":"patch"}]}`)
	response := []byte(`{"id":"chat_1","created":1,"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"apply_patch","arguments":"{\"input\":\"*** Begin Patch\"}"}}]}}]}`)

	got := ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(context.Background(), "gpt-test", request, nil, response, nil)
	if itemType := gjson.Get(got, "output.0.type").String(); itemType != "custom_tool_call" {
		t.Fatalf("restored type = %q; payload=%s", itemType, got)
	}
	if input := gjson.Get(got, "output.0.input").String(); input != "*** Begin Patch" {
		t.Fatalf("restored input = %q; payload=%s", input, got)
	}
	if gjson.Get(got, "output.0.arguments").Exists() {
		t.Fatalf("custom tool must not expose arguments: %s", got)
	}
}
