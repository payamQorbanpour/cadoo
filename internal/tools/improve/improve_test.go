package improve

import (
	"strings"
	"testing"
)

func TestImproveSystemPromptCap(t *testing.T) {
	// The system prompt must contain an explicit AT MOST constraint and must
	// not rely solely on the softer "prefer 2-5" phrasing that LLMs ignore.
	if !strings.Contains(systemPrompt, "AT MOST 5") {
		t.Error("system prompt must contain 'AT MOST 5' to enforce the suggestion cap")
	}
	if strings.Contains(systemPrompt, "Prefer 2-5") {
		t.Error("system prompt must not use soft 'Prefer 2-5' language — use AT MOST instead")
	}
}
