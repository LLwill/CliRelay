package claude

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func collectOpenAIToClaudeStream(t *testing.T, chunks ...string) string {
	t.Helper()

	originalRequest := []byte(`{"stream":true}`)
	var param any
	var out []string
	for _, chunk := range chunks {
		out = append(out, ConvertOpenAIResponseToClaude(context.Background(), "m", originalRequest, nil, []byte(chunk), &param)...)
	}
	return strings.Join(out, "")
}

func assertNoOrphanContentBlockEvents(t *testing.T, stream string) {
	t.Helper()

	openBlocks := make(map[int]bool)
	for _, segment := range strings.Split(stream, "\n\n") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}

		var eventType, data string
		for _, line := range strings.Split(segment, "\n") {
			switch {
			case strings.HasPrefix(line, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if data == "" {
			continue
		}

		payload := gjson.Parse(data)
		switch eventType {
		case "content_block_start":
			idx := int(payload.Get("index").Int())
			openBlocks[idx] = true
		case "content_block_delta":
			idx := int(payload.Get("index").Int())
			if !openBlocks[idx] {
				t.Fatalf("content_block_delta without matching content_block_start at index %d:\n%s", idx, stream)
			}
		case "content_block_stop":
			idx := int(payload.Get("index").Int())
			if !openBlocks[idx] {
				t.Fatalf("content_block_stop without matching content_block_start at index %d:\n%s", idx, stream)
			}
			delete(openBlocks, idx)
		}
	}
}

func assertAnthropicToolStreamInvariants(t *testing.T, stream string, wantToolUse bool) {
	t.Helper()

	openBlocks := make(map[int]string)
	toolUseCount := 0
	messageStopCount := 0
	var stopReasons []string

	for _, segment := range strings.Split(stream, "\n\n") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}

		var eventType, data string
		for _, line := range strings.Split(segment, "\n") {
			switch {
			case strings.HasPrefix(line, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if data == "" {
			continue
		}

		payload := gjson.Parse(data)
		switch eventType {
		case "content_block_start":
			idx := int(payload.Get("index").Int())
			blockType := payload.Get("content_block.type").String()
			if blockType == "" {
				t.Fatalf("content_block_start missing content_block.type:\n%s", stream)
			}
			if openBlocks[idx] != "" {
				t.Fatalf("content_block_start reused open index %d:\n%s", idx, stream)
			}
			if blockType == "tool_use" {
				toolUseCount++
				if strings.TrimSpace(payload.Get("content_block.name").String()) == "" {
					t.Fatalf("tool_use content_block_start has empty name:\n%s", stream)
				}
			}
			openBlocks[idx] = blockType

		case "content_block_delta":
			idx := int(payload.Get("index").Int())
			blockType := openBlocks[idx]
			if blockType == "" {
				t.Fatalf("content_block_delta without matching content_block_start at index %d:\n%s", idx, stream)
			}
			deltaType := payload.Get("delta.type").String()
			switch deltaType {
			case "input_json_delta":
				if blockType != "tool_use" {
					t.Fatalf("input_json_delta attached to %s block at index %d:\n%s", blockType, idx, stream)
				}
			case "text_delta":
				if blockType != "text" {
					t.Fatalf("text_delta attached to %s block at index %d:\n%s", blockType, idx, stream)
				}
			case "thinking_delta":
				if blockType != "thinking" {
					t.Fatalf("thinking_delta attached to %s block at index %d:\n%s", blockType, idx, stream)
				}
			default:
				t.Fatalf("unknown content_block_delta type %q:\n%s", deltaType, stream)
			}

		case "content_block_stop":
			idx := int(payload.Get("index").Int())
			if openBlocks[idx] == "" {
				t.Fatalf("content_block_stop without matching content_block_start at index %d:\n%s", idx, stream)
			}
			delete(openBlocks, idx)

		case "message_delta":
			if stopReason := payload.Get("delta.stop_reason"); stopReason.Exists() {
				stopReasons = append(stopReasons, stopReason.String())
			}

		case "message_stop":
			messageStopCount++
		}
	}

	if len(openBlocks) > 0 {
		t.Fatalf("stream ended with open content blocks %v:\n%s", openBlocks, stream)
	}
	if wantToolUse && toolUseCount == 0 {
		t.Fatalf("expected at least one valid tool_use block:\n%s", stream)
	}
	if !wantToolUse && toolUseCount != 0 {
		t.Fatalf("expected no tool_use blocks, got %d:\n%s", toolUseCount, stream)
	}
	for _, stopReason := range stopReasons {
		if wantToolUse && stopReason != "tool_use" {
			t.Fatalf("valid tool stream stop_reason = %q, want tool_use:\n%s", stopReason, stream)
		}
		if !wantToolUse && stopReason == "tool_use" {
			t.Fatalf("invalid tool stream must not stop with tool_use:\n%s", stream)
		}
	}
	if messageStopCount != 1 {
		t.Fatalf("stream should emit exactly one message_stop, got %d:\n%s", messageStopCount, stream)
	}
}

type streamToolDeltaSpec struct {
	Index   int
	ID      string
	HasName bool
	Name    string
	HasArgs bool
	Args    string
}

func openAIStreamToolDelta(spec streamToolDeltaSpec) string {
	fields := []string{fmt.Sprintf(`"index":%d`, spec.Index)}
	if spec.ID != "" {
		fields = append(fields, `"id":`+strconv.Quote(spec.ID))
	}
	fields = append(fields, `"type":"function"`)

	var functionFields []string
	if spec.HasName {
		functionFields = append(functionFields, `"name":`+strconv.Quote(spec.Name))
	}
	if spec.HasArgs {
		functionFields = append(functionFields, `"arguments":`+strconv.Quote(spec.Args))
	}
	if len(functionFields) > 0 {
		fields = append(fields, `"function":{`+strings.Join(functionFields, ",")+`}`)
	}

	return "{" + strings.Join(fields, ",") + "}"
}

func openAIStreamChunk(id string, toolDeltas []streamToolDeltaSpec, finishReason string) string {
	deltaFields := []string{}
	if len(toolDeltas) > 0 {
		var toolJSON []string
		for _, delta := range toolDeltas {
			toolJSON = append(toolJSON, openAIStreamToolDelta(delta))
		}
		deltaFields = append(deltaFields, `"tool_calls":[`+strings.Join(toolJSON, ",")+`]`)
	}

	finish := "null"
	if finishReason != "" {
		finish = strconv.Quote(finishReason)
	}

	return fmt.Sprintf(
		`data: {"id":%s,"object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{%s},"finish_reason":%s}]}`,
		strconv.Quote(id),
		strings.Join(deltaFields, ","),
		finish,
	)
}

func openAIStreamUsageChunk(id string) string {
	return fmt.Sprintf(
		`data: {"id":%s,"object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`,
		strconv.Quote(id),
	)
}

func TestOpenAIStreamingEmptyToolNameSkipsOrphanToolEvents(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-empty-tool","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_empty","type":"function","function":{"name":"","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-empty-tool","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)

	if strings.Contains(out, `"type":"tool_use"`) {
		t.Fatalf("empty function.name must not emit tool_use block:\n%s", out)
	}
	if strings.Contains(out, `"type":"input_json_delta"`) {
		t.Fatalf("empty function.name must not emit input_json_delta:\n%s", out)
	}
	if strings.Contains(out, `"type":"content_block_stop"`) {
		t.Fatalf("empty function.name must not emit orphan content_block_stop:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Fatalf("empty-only tool_calls finish should become end_turn:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingWhitespaceToolNameSkipped(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-space-tool","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_space","type":"function","function":{"name":"   ","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-space-tool","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)

	if strings.Contains(out, `"type":"tool_use"`) || strings.Contains(out, `"type":"input_json_delta"`) {
		t.Fatalf("whitespace function.name must not emit tool events:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Fatalf("whitespace-only tool_calls finish should become end_turn:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingTextWithEmptyToolNameKeepsTextOnly(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-text-empty-tool","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-text-empty-tool","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_empty","type":"function","function":{"name":"","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-text-empty-tool","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)

	if !strings.Contains(out, `"type":"text_delta","text":"hello"`) {
		t.Fatalf("text before empty-name tool call should be preserved:\n%s", out)
	}
	if strings.Contains(out, `"type":"tool_use"`) || strings.Contains(out, `"type":"input_json_delta"`) {
		t.Fatalf("empty-name tool call must not emit tool events after text:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Fatalf("empty-only tool_calls finish after text should become end_turn:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingEmptyToolNameWithUsageChunkStopReasonEndTurn(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-empty-tool-usage","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_empty","type":"function","function":{"name":"","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-empty-tool-usage","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chatcmpl-empty-tool-usage","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`,
		`data: [DONE]`,
	)

	if strings.Contains(out, `"type":"tool_use"`) || strings.Contains(out, `"type":"input_json_delta"`) {
		t.Fatalf("empty-name tool call must not emit tool events with usage chunk:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Fatalf("usage message_delta should use end_turn when no valid tool was emitted:\n%s", out)
	}
	if strings.Count(out, `"type":"message_stop"`) != 1 {
		t.Fatalf("stream should emit exactly one message_stop:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingToolArgumentsBeforeNamePreserved(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-late-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_late","type":"function","function":{"arguments":"{\"path\":\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-late-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"Read"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-late-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)

	start := strings.Index(out, `"type":"tool_use"`)
	delta := strings.Index(out, `"type":"input_json_delta"`)
	stop := strings.Index(out, `"type":"content_block_stop"`)
	if start == -1 || delta == -1 || stop == -1 {
		t.Fatalf("late valid name should emit complete tool block:\n%s", out)
	}
	if !(start < delta && delta < stop) {
		t.Fatalf("tool block events out of order:\n%s", out)
	}
	if !strings.Contains(out, `"name":"Read"`) || !strings.Contains(out, `"partial_json":"{\"path\":\"x\"}"`) {
		t.Fatalf("late valid name should preserve name and buffered arguments:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("valid tool call should keep tool_use stop_reason:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingToolArgumentsWithoutValidNameSkipped(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-missing-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_missing","type":"function","function":{"arguments":"{\"path\":\"a.go\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-missing-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"extra\":true}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-missing-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)

	if strings.Contains(out, `"type":"tool_use"`) {
		t.Fatalf("arguments without a valid function.name must not emit tool_use:\n%s", out)
	}
	if strings.Contains(out, `"type":"input_json_delta"`) {
		t.Fatalf("arguments without a valid function.name must not emit input_json_delta:\n%s", out)
	}
	if strings.Contains(out, `"type":"content_block_stop"`) {
		t.Fatalf("arguments without a valid function.name must not emit orphan content_block_stop:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Fatalf("tool_calls finish without a valid tool block should become end_turn:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingWhitespaceNameAfterBufferedArgumentsSkipped(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-blank-late-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_blank","type":"function","function":{"arguments":"{\"path\":\"a.go\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-blank-late-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"   "}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-blank-late-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"tail\":1}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-blank-late-name","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)

	if strings.Contains(out, `"type":"tool_use"`) || strings.Contains(out, `"type":"input_json_delta"`) {
		t.Fatalf("buffered arguments with whitespace-only function.name must not emit tool events:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Fatalf("whitespace-only late tool name should leave stop_reason=end_turn:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingMixedValidAndEmptyToolCalls(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-mixed-tools","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_valid","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"a.go\"}"}},{"index":1,"id":"call_empty","type":"function","function":{"name":"","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-mixed-tools","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)

	if got := strings.Count(out, `"type":"tool_use"`); got != 1 {
		t.Fatalf("expected exactly one valid tool_use, got %d:\n%s", got, out)
	}
	if strings.Contains(out, `call_empty`) {
		t.Fatalf("empty-name tool call must not be emitted:\n%s", out)
	}
	if !strings.Contains(out, `"name":"Read"`) || !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("valid tool call should be preserved:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingEmptyIndexBeforeValidIndexKeepsValidOnly(t *testing.T) {
	out := collectOpenAIToClaudeStream(t,
		`data: {"id":"chatcmpl-empty-first-index","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_empty","type":"function","function":{"arguments":"{\"path\":\"x\"}"}},{"index":1,"id":"call_valid","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"a.go\""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-empty-first-index","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":""}},{"index":1,"function":{"arguments":",\"tail\":true}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-empty-first-index","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)

	if got := strings.Count(out, `"type":"tool_use"`); got != 1 {
		t.Fatalf("expected exactly one valid tool_use, got %d:\n%s", got, out)
	}
	if strings.Contains(out, `call_empty`) {
		t.Fatalf("empty first-index tool call must not be emitted:\n%s", out)
	}
	if !strings.Contains(out, `"name":"Read"`) || !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("valid second-index tool call should be preserved:\n%s", out)
	}
	assertNoOrphanContentBlockEvents(t, out)
}

func TestOpenAIStreamingToolNameStateInvariantsGenerated(t *testing.T) {
	type namePlan struct {
		name        string
		step        int
		value       string
		expectValid bool
	}
	namePlans := []namePlan{
		{name: "absent_forever", step: -1, expectValid: false},
		{name: "empty_initial", step: 0, value: "", expectValid: false},
		{name: "blank_initial", step: 0, value: " \t ", expectValid: false},
		{name: "empty_late", step: 1, value: "", expectValid: false},
		{name: "blank_late", step: 1, value: " \n ", expectValid: false},
		{name: "valid_initial", step: 0, value: "Read", expectValid: true},
		{name: "valid_late", step: 1, value: "Read", expectValid: true},
	}
	type argPlan struct {
		name string
		args [2]string
	}
	argPlans := []argPlan{
		{name: "none"},
		{name: "initial", args: [2]string{`{"path":"a.go"}`, ""}},
		{name: "before_and_after", args: [2]string{`{"path":"a.go"`, `,"line":1}`}},
		{name: "after_only", args: [2]string{"", `{"late":true}`}},
	}
	finishReasons := []string{"tool_calls", "function_call"}
	usageVariants := []bool{false, true}

	for _, names := range namePlans {
		for _, args := range argPlans {
			for _, finishReason := range finishReasons {
				for _, withUsage := range usageVariants {
					testName := strings.Join([]string{names.name, args.name, finishReason, fmt.Sprintf("usage_%v", withUsage)}, "/")
					t.Run(testName, func(t *testing.T) {
						var chunks []string
						for step := 0; step < 2; step++ {
							spec := streamToolDeltaSpec{Index: 0, ID: "call_generated"}
							include := false
							if names.step == step {
								spec.HasName = true
								spec.Name = names.value
								include = true
							}
							if args.args[step] != "" {
								spec.HasArgs = true
								spec.Args = args.args[step]
								include = true
							}
							if include {
								chunks = append(chunks, openAIStreamChunk("chatcmpl-generated", []streamToolDeltaSpec{spec}, ""))
							}
						}
						chunks = append(chunks, openAIStreamChunk("chatcmpl-generated", nil, finishReason))
						if withUsage {
							chunks = append(chunks, openAIStreamUsageChunk("chatcmpl-generated"))
						}
						chunks = append(chunks, `data: [DONE]`)

						out := collectOpenAIToClaudeStream(t, chunks...)
						assertAnthropicToolStreamInvariants(t, out, names.expectValid)
					})
				}
			}
		}
	}
}

func TestOpenAIStreamingParallelToolNameStateInvariantsGenerated(t *testing.T) {
	invalidNames := []struct {
		name    string
		hasName bool
		value   string
	}{
		{name: "absent", hasName: false},
		{name: "empty", hasName: true, value: ""},
		{name: "blank", hasName: true, value: " \t "},
	}
	secondChunkArgs := []string{"", `,"line":1}`}

	for _, invalid := range invalidNames {
		for _, tailArgs := range secondChunkArgs {
			t.Run(invalid.name+"/tail_"+strconv.Quote(tailArgs), func(t *testing.T) {
				firstEmpty := streamToolDeltaSpec{
					Index:   0,
					ID:      "call_empty",
					HasArgs: true,
					Args:    `{"path":"x"}`,
				}
				firstValid := streamToolDeltaSpec{
					Index:   1,
					ID:      "call_valid",
					HasName: true,
					Name:    "Read",
					HasArgs: true,
					Args:    `{"file_path":"a.go"`,
				}
				secondEmpty := streamToolDeltaSpec{
					Index:   0,
					HasName: invalid.hasName,
					Name:    invalid.value,
				}
				secondValid := streamToolDeltaSpec{
					Index:   1,
					HasArgs: tailArgs != "",
					Args:    tailArgs,
				}

				chunks := []string{
					openAIStreamChunk("chatcmpl-parallel-generated", []streamToolDeltaSpec{firstEmpty, firstValid}, ""),
					openAIStreamChunk("chatcmpl-parallel-generated", []streamToolDeltaSpec{secondEmpty, secondValid}, ""),
					openAIStreamChunk("chatcmpl-parallel-generated", nil, "tool_calls"),
					`data: [DONE]`,
				}
				out := collectOpenAIToClaudeStream(t, chunks...)
				assertAnthropicToolStreamInvariants(t, out, true)
				if strings.Contains(out, "call_empty") {
					t.Fatalf("empty parallel tool call leaked into Claude stream:\n%s", out)
				}
				if got := strings.Count(out, `"type":"tool_use"`); got != 1 {
					t.Fatalf("expected only one valid tool_use, got %d:\n%s", got, out)
				}
			})
		}
	}
}

func TestOpenAINonStreamingEmptyToolNameSkippedAndStopReasonEndTurn(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl-empty-tool","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_empty","type":"function","function":{"name":"","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`)

	streamCompat := strings.Join(convertOpenAINonStreamingToAnthropic(raw), "")
	if strings.Contains(streamCompat, `"type":"tool_use"`) || strings.Contains(streamCompat, `"name":""`) {
		t.Fatalf("empty function.name must not emit non-streaming tool_use:\n%s", streamCompat)
	}
	if got := gjson.Get(streamCompat, "stop_reason").String(); got != "end_turn" {
		t.Fatalf("stream-compatible non-stream stop_reason = %q, want end_turn; payload=%s", got, streamCompat)
	}

	nonStream := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "m", nil, nil, raw, nil)
	if strings.Contains(nonStream, `"type":"tool_use"`) || strings.Contains(nonStream, `"name":""`) {
		t.Fatalf("empty function.name must not emit non-streaming tool_use:\n%s", nonStream)
	}
	if got := gjson.Get(nonStream, "stop_reason").String(); got != "end_turn" {
		t.Fatalf("non-stream stop_reason = %q, want end_turn; payload=%s", got, nonStream)
	}
}

func TestOpenAINonStreamingUnwrapsProviderDataEnvelope(t *testing.T) {
	raw := []byte(`{"success":true,"data":{"id":"chatcmpl_wrapped","object":"chat.completion","created":1,"model":"cline-pass/qwen3.7-max","choices":[{"index":0,"message":{"role":"assistant","content":"wrapped ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}}`)

	out := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "m", nil, nil, raw, nil)
	if got := gjson.Get(out, "content.0.text").String(); got != "wrapped ok" {
		t.Fatalf("text = %q, want wrapped ok; payload=%s", got, out)
	}
	if got := gjson.Get(out, "usage.input_tokens").Int(); got != 11 {
		t.Fatalf("input tokens = %d, want 11; payload=%s", got, out)
	}
	if got := gjson.Get(out, "usage.output_tokens").Int(); got != 7 {
		t.Fatalf("output tokens = %d, want 7; payload=%s", got, out)
	}
}

func TestOpenAINonStreamingContentArrayEmptyToolNameSkipped(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl-empty-tool-array","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"tool_calls","tool_calls":[{"id":"call_empty","type":"function","function":{"name":"","arguments":"{\"path\":\"x\"}"}}]}]},"finish_reason":"tool_calls"}]}`)

	out := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "m", nil, nil, raw, nil)
	if strings.Contains(out, `"type":"tool_use"`) || strings.Contains(out, `"name":""`) {
		t.Fatalf("empty function.name in content-array tool_calls must be skipped:\n%s", out)
	}
	if got := gjson.Get(out, "stop_reason").String(); got != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn; payload=%s", got, out)
	}
}

func TestOpenAINonStreamingToolNameStateInvariantsGenerated(t *testing.T) {
	names := []struct {
		name        string
		field       string
		expectValid bool
	}{
		{name: "missing", field: "", expectValid: false},
		{name: "empty", field: `"name":"",`, expectValid: false},
		{name: "blank", field: `"name":" \t ",`, expectValid: false},
		{name: "valid", field: `"name":"Read",`, expectValid: true},
	}
	contentShapes := []struct {
		name     string
		template string
	}{
		{
			name:     "message_tool_calls",
			template: `{"id":"chatcmpl-%s","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_%s","type":"function","function":{%s"arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
		},
		{
			name:     "content_array_tool_calls",
			template: `{"id":"chatcmpl-%s","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"tool_calls","tool_calls":[{"id":"call_%s","type":"function","function":{%s"arguments":"{\"path\":\"x\"}"}}]}]},"finish_reason":"tool_calls"}]}`,
		},
	}

	for _, shape := range contentShapes {
		for _, tc := range names {
			t.Run(shape.name+"/"+tc.name, func(t *testing.T) {
				raw := []byte(fmt.Sprintf(shape.template, tc.name, tc.name, tc.field))
				out := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "m", nil, nil, raw, nil)
				hasToolUse := strings.Contains(out, `"type":"tool_use"`)
				if hasToolUse != tc.expectValid {
					t.Fatalf("tool_use presence = %v, want %v: %s", hasToolUse, tc.expectValid, out)
				}
				if tc.expectValid {
					if got := gjson.Get(out, "content.#(type==\"tool_use\").name").String(); got != "Read" {
						t.Fatalf("valid tool_use name = %q, want Read: %s", got, out)
					}
					if got := gjson.Get(out, "stop_reason").String(); got != "tool_use" {
						t.Fatalf("valid tool stop_reason = %q, want tool_use: %s", got, out)
					}
				} else {
					if strings.Contains(out, `"name":""`) || strings.Contains(out, `"name":" `) {
						t.Fatalf("invalid tool name leaked into Claude response: %s", out)
					}
					if got := gjson.Get(out, "stop_reason").String(); got == "tool_use" {
						t.Fatalf("invalid tool stop_reason must not be tool_use: %s", out)
					}
				}
			})
		}
	}
}
