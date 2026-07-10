package thinking

import (
	"testing"
)

func TestStripBracketSuffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bracket 1M suffix", "deepseek-v4-flash[1M]", "deepseek-v4-flash"},
		{"bracket 128K suffix", "model[128K]", "model"},
		{"bracket 32k lowercase", "model[32k]", "model"},
		{"bracket 4096 digits only", "model[4096]", "model"},
		{"bracket 1m lowercase m", "model[1m]", "model"},
		{"no bracket", "gemini-2.5-pro", "gemini-2.5-pro"},
		{"empty string", "", ""},
		{"bracket with thinking suffix", "model[1M](8192)", "model[1M](8192)"},
		{"thinking suffix only", "model(8192)", "model(8192)"},
		{"non-marker beta preserved", "model[beta]", "model[beta]"},
		{"non-marker preview preserved", "model[preview]", "model[preview]"},
		{"mixed: marker then non-marker", "model[1M][test]", "model[1M][test]"},
		{"no closing bracket", "model[1M", "model[1M"},
		{"no opening bracket", "model]", "model]"},
		{"empty brackets", "model[]", "model[]"},
		{"bracket at start", "[1M]model", "[1M]model"},
		{"text after bracket", "model[1M]extra", "model[1M]extra"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripBracketSuffix(tt.input)
			if got != tt.want {
				t.Errorf("StripBracketSuffix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSuffix_BracketSuffix(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantModelName string
		wantHasSuffix bool
		wantRawSuffix string
	}{
		{
			name:          "deepseek-v4-flash with 1M bracket",
			input:         "deepseek-v4-flash[1M]",
			wantModelName: "deepseek-v4-flash",
			wantHasSuffix: false,
			wantRawSuffix: "",
		},
		{
			name:          "model with 128K bracket",
			input:         "claude-sonnet-4-5[128K]",
			wantModelName: "claude-sonnet-4-5",
			wantHasSuffix: false,
			wantRawSuffix: "",
		},
		{
			name:          "bracket before thinking suffix",
			input:         "model[1M](8192)",
			wantModelName: "model",
			wantHasSuffix: true,
			wantRawSuffix: "8192",
		},
		{
			name:          "thinking suffix then bracket",
			input:         "model(8192)[1M]",
			wantModelName: "model",
			wantHasSuffix: true,
			wantRawSuffix: "8192",
		},
		{
			name:          "round bracket suffix unchanged",
			input:         "gemini-2.5-pro(8192)",
			wantModelName: "gemini-2.5-pro",
			wantHasSuffix: true,
			wantRawSuffix: "8192",
		},
		{
			name:          "level suffix unchanged",
			input:         "gpt-5.2(high)",
			wantModelName: "gpt-5.2",
			wantHasSuffix: true,
			wantRawSuffix: "high",
		},
		{
			name:          "no suffix unchanged",
			input:         "claude-sonnet-4-5",
			wantModelName: "claude-sonnet-4-5",
			wantHasSuffix: false,
			wantRawSuffix: "",
		},
		{
			name:          "special suffix none",
			input:         "gemini-2.5-flash(none)",
			wantModelName: "gemini-2.5-flash",
			wantHasSuffix: true,
			wantRawSuffix: "none",
		},
		{
			name:          "special suffix auto",
			input:         "claude-sonnet-4-5(auto)",
			wantModelName: "claude-sonnet-4-5",
			wantHasSuffix: true,
			wantRawSuffix: "auto",
		},
		{
			name:          "non-marker bracket preserved",
			input:         "custom-model[beta]",
			wantModelName: "custom-model[beta]",
			wantHasSuffix: false,
			wantRawSuffix: "",
		},
		{
			name:          "non-marker bracket with thinking suffix preserved",
			input:         "custom-model[beta](8192)",
			wantModelName: "custom-model[beta]",
			wantHasSuffix: true,
			wantRawSuffix: "8192",
		},
		{
			name:          "bracket with trailing whitespace",
			input:         " model[1M]",
			wantModelName: " model",
			wantHasSuffix: false,
			wantRawSuffix: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSuffix(tt.input)
			if result.ModelName != tt.wantModelName {
				t.Errorf("ParseSuffix(%q).ModelName = %q, want %q", tt.input, result.ModelName, tt.wantModelName)
			}
			if result.HasSuffix != tt.wantHasSuffix {
				t.Errorf("ParseSuffix(%q).HasSuffix = %v, want %v", tt.input, result.HasSuffix, tt.wantHasSuffix)
			}
			if result.RawSuffix != tt.wantRawSuffix {
				t.Errorf("ParseSuffix(%q).RawSuffix = %q, want %q", tt.input, result.RawSuffix, tt.wantRawSuffix)
			}
		})
	}
}

func TestParseLevelSuffixMaxButNotUltra(t *testing.T) {
	if level, ok := ParseLevelSuffix("max"); !ok || level != LevelMax {
		t.Fatalf("ParseLevelSuffix(max) = (%q, %v), want (%q, true)", level, ok, LevelMax)
	}
	if level, ok := ParseLevelSuffix("ultra"); ok || level != "" {
		t.Fatalf("ParseLevelSuffix(ultra) = (%q, %v), want (\"\", false)", level, ok)
	}
}
