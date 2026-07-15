package responses

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type responsesToolMetadata struct {
	Name      string
	Namespace string
	Custom    bool
}

func convertResponsesToolToOpenAIChatTools(tool gjson.Result) [][]byte {
	toolType := strings.TrimSpace(tool.Get("type").String())
	switch toolType {
	case "", "function":
		if converted, ok := convertResponsesFunctionToolToOpenAIChat(tool, ""); ok {
			return [][]byte{converted}
		}
	case "namespace":
		return convertResponsesNamespaceToolToOpenAIChat(tool)
	case "custom":
		if converted, ok := convertResponsesCustomToolToOpenAIChat(tool, ""); ok {
			return [][]byte{converted}
		}
	}
	return nil
}

func convertResponsesFunctionToolToOpenAIChat(tool gjson.Result, overrideName string) ([]byte, bool) {
	name := strings.TrimSpace(overrideName)
	if name == "" {
		name = responsesToolName(tool)
	}
	if name == "" {
		return nil, false
	}

	converted := []byte(`{"type":"function","function":{"name":"","description":"","parameters":{}}}`)
	converted, _ = sjson.SetBytes(converted, "function.name", name)
	if description := responsesToolDescription(tool); description != "" {
		converted, _ = sjson.SetBytes(converted, "function.description", description)
	}
	if parameters := responsesToolParameters(tool); parameters.Exists() {
		converted, _ = sjson.SetRawBytes(converted, "function.parameters", []byte(parameters.Raw))
	}
	if strict := tool.Get("strict"); strict.Exists() {
		converted, _ = sjson.SetBytes(converted, "function.strict", strict.Bool())
	}
	return converted, true
}

// Responses custom tools accept a free-form string. Chat Completions has no
// equivalent, so the value is carried in a single required "input" property.
func convertResponsesCustomToolToOpenAIChat(tool gjson.Result, overrideName string) ([]byte, bool) {
	name := strings.TrimSpace(overrideName)
	if name == "" {
		name = responsesToolName(tool)
	}
	if name == "" {
		return nil, false
	}

	converted := []byte(`{"type":"function","function":{"name":"","description":"","parameters":{"type":"object","properties":{"input":{"type":"string"}},"required":["input"],"additionalProperties":false}}}`)
	converted, _ = sjson.SetBytes(converted, "function.name", name)
	if description := responsesToolDescription(tool); description != "" {
		converted, _ = sjson.SetBytes(converted, "function.description", description)
	}
	return converted, true
}

func convertResponsesNamespaceToolToOpenAIChat(tool gjson.Result) [][]byte {
	namespace := strings.TrimSpace(tool.Get("name").String())
	children := tool.Get("tools")
	if namespace == "" || !children.Exists() || !children.IsArray() {
		return nil
	}

	var converted [][]byte
	children.ForEach(func(_, child gjson.Result) bool {
		qualifiedName := qualifyResponsesNamespaceToolName(namespace, responsesToolName(child))
		switch strings.TrimSpace(child.Get("type").String()) {
		case "", "function":
			if item, ok := convertResponsesFunctionToolToOpenAIChat(child, qualifiedName); ok {
				converted = append(converted, item)
			}
		case "custom":
			if item, ok := convertResponsesCustomToolToOpenAIChat(child, qualifiedName); ok {
				converted = append(converted, item)
			}
		}
		return true
	})
	return converted
}

func responsesToolName(tool gjson.Result) string {
	if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
		return name
	}
	return strings.TrimSpace(tool.Get("function.name").String())
}

func responsesToolDescription(tool gjson.Result) string {
	if description := tool.Get("description").String(); description != "" {
		return description
	}
	return tool.Get("function.description").String()
}

func responsesToolParameters(tool gjson.Result) gjson.Result {
	for _, path := range []string{
		"parameters",
		"parametersJsonSchema",
		"input_schema",
		"function.parameters",
		"function.parametersJsonSchema",
	} {
		if parameters := tool.Get(path); parameters.Exists() {
			return parameters
		}
	}
	return gjson.Result{}
}

func qualifyResponsesNamespaceToolName(namespace, name string) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" || name == "" || strings.HasPrefix(name, "mcp__") {
		return name
	}
	if strings.HasPrefix(name, namespace+"__") {
		return name
	}
	if strings.HasSuffix(namespace, "__") {
		return namespace + name
	}
	return namespace + "__" + name
}

func responsesRequestJSON(originalRequestRawJSON, requestRawJSON []byte) []byte {
	if len(originalRequestRawJSON) > 0 && gjson.ValidBytes(originalRequestRawJSON) {
		return originalRequestRawJSON
	}
	if len(requestRawJSON) > 0 && gjson.ValidBytes(requestRawJSON) {
		return requestRawJSON
	}
	return nil
}

func forEachResponsesRequestTool(requestRawJSON []byte, visit func(tool gjson.Result, namespace string) bool) {
	if len(requestRawJSON) == 0 || !gjson.ValidBytes(requestRawJSON) {
		return
	}
	root := gjson.ParseBytes(requestRawJSON)
	var walk func(gjson.Result, string) bool
	walk = func(tools gjson.Result, namespace string) bool {
		if !tools.Exists() || !tools.IsArray() {
			return true
		}
		keepGoing := true
		tools.ForEach(func(_, tool gjson.Result) bool {
			if strings.TrimSpace(tool.Get("type").String()) == "namespace" {
				childNamespace := strings.TrimSpace(tool.Get("name").String())
				if !walk(tool.Get("tools"), childNamespace) {
					keepGoing = false
					return false
				}
				return true
			}
			keepGoing = visit(tool, namespace)
			return keepGoing
		})
		return keepGoing
	}
	if !walk(root.Get("tools"), "") {
		return
	}
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			if item.Get("type").String() == "additional_tools" {
				return walk(item.Get("tools"), "")
			}
			return true
		})
	}
}

func responsesToolMetadataForName(requestRawJSON []byte, qualifiedName string) responsesToolMetadata {
	qualifiedName = strings.TrimSpace(qualifiedName)
	metadata := responsesToolMetadata{Name: qualifiedName}
	forEachResponsesRequestTool(requestRawJSON, func(tool gjson.Result, namespace string) bool {
		name := responsesToolName(tool)
		candidate := qualifyResponsesNamespaceToolName(namespace, name)
		if candidate != qualifiedName {
			return true
		}
		metadata.Name = name
		metadata.Namespace = namespace
		metadata.Custom = strings.TrimSpace(tool.Get("type").String()) == "custom"
		return false
	})
	return metadata
}

func responsesCustomToolNames(requestRawJSON []byte) map[string]struct{} {
	names := make(map[string]struct{})
	forEachResponsesRequestTool(requestRawJSON, func(tool gjson.Result, namespace string) bool {
		if strings.TrimSpace(tool.Get("type").String()) == "custom" {
			if name := qualifyResponsesNamespaceToolName(namespace, responsesToolName(tool)); name != "" {
				names[name] = struct{}{}
			}
		}
		return true
	})
	return names
}

func responsesToolOutputText(output gjson.Result) string {
	if output.Type == gjson.String {
		return output.String()
	}
	if output.IsArray() {
		var text strings.Builder
		output.ForEach(func(_, part gjson.Result) bool {
			if part.Type == gjson.String {
				text.WriteString(part.String())
			} else if value := part.Get("text"); value.Exists() {
				text.WriteString(value.String())
			}
			return true
		})
		return text.String()
	}
	if output.Exists() {
		return output.Raw
	}
	return ""
}

func unwrapResponsesCustomToolInput(arguments string) string {
	if input := gjson.Get(arguments, "input"); input.Exists() {
		if input.Type == gjson.String {
			return input.String()
		}
		return input.Raw
	}
	return arguments
}

func applyResponsesToolCallMetadata(item, itemPath string, metadata responsesToolMetadata, arguments string) string {
	prefix := ""
	if itemPath != "" {
		prefix = strings.TrimSuffix(itemPath, ".") + "."
	}
	item, _ = sjson.Set(item, prefix+"name", metadata.Name)
	if metadata.Namespace != "" {
		item, _ = sjson.Set(item, prefix+"namespace", metadata.Namespace)
	}
	if metadata.Custom {
		item, _ = sjson.Set(item, prefix+"type", "custom_tool_call")
		item, _ = sjson.Delete(item, prefix+"arguments")
		item, _ = sjson.Set(item, prefix+"input", unwrapResponsesCustomToolInput(arguments))
	} else {
		item, _ = sjson.Set(item, prefix+"type", "function_call")
		item, _ = sjson.Set(item, prefix+"arguments", arguments)
	}
	return item
}

func wrapResponsesCustomToolInput(input string) string {
	wrapper := `{"input":""}`
	wrapper, _ = sjson.Set(wrapper, "input", input)
	return wrapper
}
