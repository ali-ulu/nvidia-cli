package skills

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ali-ulu/nvidia-cli/internal/guidelines"
)

func TestAllIsSortedAndNonEmpty(t *testing.T) {
	all := All()
	if len(all) < 2 {
		t.Fatalf("expected several embedded skills, got %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name >= all[i].Name {
			t.Errorf("All() not sorted: %q before %q", all[i-1].Name, all[i].Name)
		}
	}
	for _, s := range all {
		if s.Name == "" || s.Description == "" || s.System == "" {
			t.Errorf("skill has an empty field: %+v", s)
		}
	}
}

func TestAllReturnsACopy(t *testing.T) {
	got := All()
	got[0].Name = "mutated"
	if again := All(); again[0].Name == "mutated" {
		t.Error("All() exposed the internal slice to mutation")
	}
}

func TestLeanCodingSkillCarriesGuidelines(t *testing.T) {
	s, ok := Get("lean-coding")
	if !ok {
		t.Fatal("lean-coding skill missing")
	}
	if !strings.Contains(s.System, "engineering guidelines") {
		t.Errorf("lean-coding skill does not wrap the guidelines: %q", s.System[:60])
	}
	if !strings.Contains(s.System, strings.TrimSpace(guidelines.Text())[:40]) {
		t.Error("lean-coding skill text does not embed the guidelines document")
	}
}

func TestGetUnknownIsFalse(t *testing.T) {
	if _, ok := Get("nope"); ok {
		t.Error("Get returned ok for an unknown skill")
	}
}

func TestCombineJoinsInOrder(t *testing.T) {
	got := Combine([]string{"concise", "turkish"})
	first := mustGet(t, "concise").System
	second := mustGet(t, "turkish").System
	if want := first + "\n\n" + second; got != want {
		t.Errorf("Combine order/content wrong:\n got %q\nwant %q", got, want)
	}
}

func TestCombineSkipsUnknown(t *testing.T) {
	got := Combine([]string{"nope", "concise"})
	if want := mustGet(t, "concise").System; got != want {
		t.Errorf("Combine = %q, want the known skill only", got)
	}
}

func TestCombineEmpty(t *testing.T) {
	if got := Combine(nil); got != "" {
		t.Errorf("Combine(nil) = %q, want empty", got)
	}
}

func TestCombineSingleHasNoSeparator(t *testing.T) {
	if got := Combine([]string{"turkish"}); strings.Contains(got, "\n\n") {
		t.Errorf("single skill should not add a separator: %q", got)
	}
}

func TestNamesDedupesAndDropsUnknown(t *testing.T) {
	got := Names([]string{"concise", "nope", "concise", "turkish"})
	want := []string{"concise", "turkish"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

func TestPrintListsEverySkill(t *testing.T) {
	var buf bytes.Buffer
	Print(&buf)
	out := buf.String()
	if !strings.Contains(out, "SKILL") || !strings.Contains(out, "DESCRIPTION") {
		t.Errorf("Print missing header:\n%s", out)
	}
	for _, s := range All() {
		if !strings.Contains(out, s.Name) {
			t.Errorf("Print missing skill %q", s.Name)
		}
	}
}

func TestPrintJSONIsValidAndComplete(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintJSON(&buf); err != nil {
		t.Fatalf("PrintJSON: %v", err)
	}
	var got []Skill
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(got) != len(All()) {
		t.Errorf("JSON has %d skills, want %d", len(got), len(All()))
	}
	// System is json:"-", so it must never leak into the JSON output.
	if strings.Contains(buf.String(), `"System"`) {
		t.Error("PrintJSON leaked the System field")
	}
}

func mustGet(t *testing.T, name string) Skill {
	t.Helper()
	s, ok := Get(name)
	if !ok {
		t.Fatalf("skill %q missing", name)
	}
	return s
}

func TestAutoSelectsByTrigger(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   []string
	}{
		{"code", "write a Go function to parse this", []string{"lean-coding"}},
		{"shell", "what does chmod 644 do", []string{"shell-safe"}},
		{"brief", "explain it briefly", []string{"concise"}},
		{"turkish only", "merhaba nasılsın", []string{"turkish"}},
		{"turkish code", "bu kodda hata var mı", []string{"lean-coding", "turkish"}},
		{"nothing", "just say hello", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Auto(tc.prompt)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("Auto(%q) = %v, want %v", tc.prompt, got, tc.want)
			}
		})
	}
}

func TestAutoIsSorted(t *testing.T) {
	got := Auto("kısaca bir bash script yaz ve fonksiyon kullan")
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("Auto not sorted: %v", got)
		}
	}
}

func TestAutoCaseInsensitive(t *testing.T) {
	if got := Auto("Write a BASH script"); len(got) == 0 || got[0] != "shell-safe" {
		t.Errorf("Auto did not match uppercase trigger: %v", got)
	}
}

func TestLooksTurkishViaLetters(t *testing.T) {
	if got := Auto("işlem tamam"); len(got) == 0 {
		t.Error("Turkish letters should trigger the turkish skill")
	}
}

func TestAutoEnglishPromptIsNotTurkish(t *testing.T) {
	for _, name := range Auto("write a function that returns the sum of two numbers") {
		if name == "turkish" {
			t.Error("an English prompt was detected as Turkish")
		}
	}
}

func TestAutoNoFalseTurkishOnShortEnglish(t *testing.T) {
	// "can" must not match the word "can"; "var" would, so use a safe prompt.
	for _, name := range Auto("run the tests now") {
		if name == "turkish" {
			t.Error("a plain English prompt was detected as Turkish")
		}
	}
}
