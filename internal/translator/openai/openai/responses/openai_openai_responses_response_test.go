package responses

import (
	"context"
	"strings"
	"testing"
)

func TestConvertOpenAIChatCompletionsResponseToOpenAIResponsesPreservesParallelToolCalls(t *testing.T) {
	var param any
	chunks := [][]byte{
		[]byte(`data: {"id":"chatcmpl-parallel","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"one\""}},{"index":1,"id":"call_b","type":"function","function":{"name":"update_plan","arguments":"{\"plan\":"}}]},"finish_reason":null}]}`),
		[]byte(`data: {"id":"chatcmpl-parallel","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}},{"index":1,"function":{"arguments":"\"two\"}"}}]},"finish_reason":null}]}`),
		[]byte(`data: {"id":"chatcmpl-parallel","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`),
	}

	var out strings.Builder
	for _, chunk := range chunks {
		for _, event := range ConvertOpenAIChatCompletionsResponseToOpenAIResponses(context.Background(), "m", nil, nil, chunk, &param) {
			out.WriteString(event)
			out.WriteByte('\n')
		}
	}
	got := out.String()
	if strings.Count(got, "event: response.function_call_arguments.done") != 2 {
		t.Fatalf("expected two completed function calls:\n%s", got)
	}
	for _, want := range []string{
		`"call_id":"call_a"`,
		`"name":"exec_command"`,
		`"arguments":"{\"cmd\":\"one\"}"`,
		`"call_id":"call_b"`,
		`"name":"update_plan"`,
		`"arguments":"{\"plan\":\"two\"}"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in converted events:\n%s", want, got)
		}
	}
}

func TestConvertOpenAIChatCompletionsResponseToOpenAIResponsesUnwrapsProviderDataEnvelope(t *testing.T) {
	raw := []byte(`{"success":true,"data":{"id":"chatcmpl_wrapped","object":"chat.completion","created":1,"model":"cline-pass/qwen3.7-max","choices":[{"index":0,"message":{"role":"assistant","content":"wrapped response ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}}`)

	out := ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(context.Background(), "m", nil, nil, raw, nil)
	for _, want := range []string{
		`"model":"cline-pass/qwen3.7-max"`,
		`"text":"wrapped response ok"`,
		`"input_tokens":3`,
		`"output_tokens":4`,
		`"total_tokens":7`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in converted response:\n%s", want, out)
		}
	}
}
