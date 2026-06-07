package thinking

// ThinkingMode represents the type of thinking configuration mode.
type ThinkingMode int

const (
	// ModeBudget indicates using a numeric budget.
	ModeBudget ThinkingMode = iota
	// ModeLevel indicates using a discrete level.
	ModeLevel
	// ModeNone indicates thinking is disabled.
	ModeNone
	// ModeAuto indicates automatic/dynamic thinking.
	ModeAuto
)

// String returns the string representation of ThinkingMode.
func (m ThinkingMode) String() string {
	switch m {
	case ModeBudget:
		return "budget"
	case ModeLevel:
		return "level"
	case ModeNone:
		return "none"
	case ModeAuto:
		return "auto"
	default:
		return "unknown"
	}
}

// ThinkingLevel represents a discrete thinking level.
type ThinkingLevel string

const (
	LevelNone    ThinkingLevel = "none"
	LevelAuto    ThinkingLevel = "auto"
	LevelMinimal ThinkingLevel = "minimal"
	LevelLow     ThinkingLevel = "low"
	LevelMedium  ThinkingLevel = "medium"
	LevelHigh    ThinkingLevel = "high"
	LevelXHigh   ThinkingLevel = "xhigh"
)

// ThinkingConfig represents a unified thinking configuration.
type ThinkingConfig struct {
	Mode   ThinkingMode
	Budget int
	Level  ThinkingLevel
}

// SuffixResult represents the result of parsing a model name for a thinking suffix.
type SuffixResult struct {
	ModelName string
	HasSuffix bool
	RawSuffix string
}
