package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ali-ulu/nvidia-cli/internal/nim"
)

var sample = []nim.Model{
	{ID: "meta/llama-3.1-8b-instruct", OwnedBy: "meta"},
	{ID: "nvidia/nemotron-4-340b", OwnedBy: "nvidia"},
}

func TestPrintModelsListsAll(t *testing.T) {
	var buf bytes.Buffer
	PrintModels(&buf, sample, "")
	out := buf.String()
	for _, m := range sample {
		if !strings.Contains(out, m.ID) || !strings.Contains(out, m.OwnedBy) {
			t.Errorf("output missing %q:\n%s", m.ID, out)
		}
	}
	if !strings.HasPrefix(out, "MODEL") {
		t.Errorf("want MODEL header first:\n%s", out)
	}
}

func TestPrintModelsFiltersCaseInsensitively(t *testing.T) {
	var buf bytes.Buffer
	PrintModels(&buf, sample, "NEMOTRON")
	out := buf.String()
	if !strings.Contains(out, "nvidia/nemotron-4-340b") {
		t.Errorf("filter dropped the match:\n%s", out)
	}
	if strings.Contains(out, "llama") {
		t.Errorf("filter kept a non-match:\n%s", out)
	}
}

func TestPrintModelsReportsEmptyMatch(t *testing.T) {
	var buf bytes.Buffer
	PrintModels(&buf, sample, "no-such-model")
	if !strings.Contains(buf.String(), "no models matched") {
		t.Errorf("want an empty-result notice:\n%s", buf.String())
	}
}
