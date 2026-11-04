package guidelines

import (
	"strings"
	"testing"
)

func TestTextIsEmbedded(t *testing.T) {
	if len(Text()) < 500 {
		t.Fatalf("embedded guidelines look truncated: %d bytes", len(Text()))
	}
}

func TestTextCarriesBothSources(t *testing.T) {
	// One signature rule from each upstream source must survive the merge.
	for _, marker := range []string{
		"Think before coding",  // Karpathy
		"shortcut:",            // Ponytail
		"Report the shortfall", // Ponytail closer
	} {
		if !strings.Contains(Text(), marker) {
			t.Errorf("merged guidelines lost %q", marker)
		}
	}
}

func TestSystemPromptWrapsText(t *testing.T) {
	got := SystemPrompt()
	if !strings.HasPrefix(got, "Follow these engineering guidelines") {
		t.Errorf("system prompt lacks the instruction prefix: %q", got[:60])
	}
	if !strings.Contains(got, strings.TrimSpace(Text())) {
		t.Error("system prompt does not contain the guidelines text")
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("system prompt should be trimmed")
	}
}
