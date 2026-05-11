package tools

import (
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
)

func TestEffectivePromptDefault(t *testing.T) {
	got := EffectivePrompt("review", "DEFAULT", config.Default())
	if !strings.Contains(got, "DEFAULT") {
		t.Errorf("expected default content, got %q", got)
	}
	if !strings.Contains(got, "Strictness: BALANCED") {
		t.Errorf("expected balanced strictness section, got %q", got)
	}
}

func TestEffectivePromptOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Prompts = map[string]config.PromptCustomization{
		"review": {Override: "MY OVERRIDE"},
	}
	got := EffectivePrompt("review", "DEFAULT", cfg)
	if strings.Contains(got, "DEFAULT") {
		t.Errorf("override should drop default, got %q", got)
	}
	if !strings.Contains(got, "MY OVERRIDE") {
		t.Error("override missing")
	}
	if !strings.Contains(got, "Strictness:") {
		t.Error("strictness should still apply over an override")
	}
}

func TestEffectivePromptAddendum(t *testing.T) {
	cfg := config.Default()
	cfg.Prompts = map[string]config.PromptCustomization{
		"review": {Addendum: "Always cite RFCs."},
	}
	got := EffectivePrompt("review", "DEFAULT", cfg)
	if !strings.Contains(got, "DEFAULT") || !strings.Contains(got, "Always cite RFCs") {
		t.Errorf("expected default + addendum, got %q", got)
	}
}

func TestEffectivePromptOverrideBeatsAddendum(t *testing.T) {
	cfg := config.Default()
	cfg.Prompts = map[string]config.PromptCustomization{
		"review": {Override: "FORCE", Addendum: "ignored"},
	}
	got := EffectivePrompt("review", "DEFAULT", cfg)
	if strings.Contains(got, "ignored") {
		t.Error("override should win over addendum")
	}
}

func TestStrictnessSectionLevels(t *testing.T) {
	cases := map[string]string{
		"minimal":  "MINIMAL",
		"balanced": "BALANCED",
		"strict":   "STRICT",
		"pedantic": "PEDANTIC",
		"":         "BALANCED",
	}
	for in, want := range cases {
		got := StrictnessSection(in)
		if !strings.Contains(got, want) {
			t.Errorf("StrictnessSection(%q) missing %q: got %q", in, want, got)
		}
	}
	if got := StrictnessSection("nonsense"); got != "" {
		t.Errorf("unknown level should yield empty, got %q", got)
	}
}
