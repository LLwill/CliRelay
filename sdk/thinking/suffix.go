package thinking

import (
	"strconv"
	"strings"
)

func isContextWindowSuffix(content string) bool {
	if content == "" {
		return false
	}
	i := 0
	for i < len(content) && content[i] >= '0' && content[i] <= '9' {
		i++
	}
	if i == 0 {
		return false
	}
	if i < len(content) {
		switch content[i] {
		case 'k', 'K', 'm', 'M':
			i++
		default:
			return false
		}
	}
	return i == len(content)
}

// StripBracketSuffix strips a trailing context-window marker like "[1M]" or "[128K]".
func StripBracketSuffix(model string) string {
	lastOpen := strings.LastIndex(model, "[")
	if lastOpen == -1 {
		return model
	}
	if !strings.HasSuffix(model, "]") {
		return model
	}
	if lastOpen+1 >= len(model)-1 {
		return model
	}
	content := model[lastOpen+1 : len(model)-1]
	if !isContextWindowSuffix(content) {
		return model
	}
	return model[:lastOpen]
}

// ParseSuffix extracts a trailing thinking suffix from a model name.
func ParseSuffix(model string) SuffixResult {
	model = StripBracketSuffix(model)
	lastOpen := strings.LastIndex(model, "(")
	if lastOpen == -1 {
		return SuffixResult{ModelName: model, HasSuffix: false}
	}
	if !strings.HasSuffix(model, ")") {
		return SuffixResult{ModelName: model, HasSuffix: false}
	}
	modelName := StripBracketSuffix(model[:lastOpen])
	rawSuffix := model[lastOpen+1 : len(model)-1]
	return SuffixResult{
		ModelName: modelName,
		HasSuffix: true,
		RawSuffix: rawSuffix,
	}
}

// ParseNumericSuffix parses a non-negative thinking budget value.
func ParseNumericSuffix(rawSuffix string) (budget int, ok bool) {
	if rawSuffix == "" {
		return 0, false
	}
	value, err := strconv.Atoi(rawSuffix)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

// ParseSpecialSuffix parses special thinking mode suffixes like "none" or "auto".
func ParseSpecialSuffix(rawSuffix string) (mode ThinkingMode, ok bool) {
	if rawSuffix == "" {
		return ModeBudget, false
	}
	switch strings.ToLower(rawSuffix) {
	case "none":
		return ModeNone, true
	case "auto", "-1":
		return ModeAuto, true
	default:
		return ModeBudget, false
	}
}

// ParseLevelSuffix parses a discrete thinking level suffix.
func ParseLevelSuffix(rawSuffix string) (level ThinkingLevel, ok bool) {
	if rawSuffix == "" {
		return "", false
	}
	switch strings.ToLower(rawSuffix) {
	case "minimal":
		return LevelMinimal, true
	case "low":
		return LevelLow, true
	case "medium":
		return LevelMedium, true
	case "high":
		return LevelHigh, true
	case "xhigh":
		return LevelXHigh, true
	default:
		return "", false
	}
}
