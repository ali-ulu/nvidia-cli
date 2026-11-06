// Package skills holds the named system prompts (skills) embedded in the nv
// binary. A skill is a reusable instruction block that ask/repl can attach to
// a request with --skill NAME, independently of the user's own --system text.
package skills

import (
	"sort"
	"strings"

	"github.com/ali-ulu/nvidia-cli/internal/guidelines"
)

// Skill is one embedded, named system prompt.
type Skill struct {
	// Name is the stable identifier used with --skill.
	Name string `json:"name"`
	// Description is a one-line summary shown by `nv skills`.
	Description string `json:"description"`
	// System is the text injected as (part of) the system message.
	System string `json:"-"`
	// Triggers are lowercase substrings that auto-attach the skill when a
	// prompt contains one (see Auto). The turkish skill has none: its language
	// is detected instead.
	Triggers []string `json:"-"`
}

// all is the registry of embedded skills. Order here is the order they are
// combined in when several are attached to one request.
var all = []Skill{
	{
		Name:        "lean-coding",
		Description: "Lean-coding discipline for code answers (the embedded guidelines).",
		System:      guidelines.SystemPrompt(),
		Triggers: []string{
			"code", "function", "implement", "refactor", "compile", "syntax",
			"debug", "regex", "unit test", "api", "kod", "fonksiyon", "derle",
			"sözdizim", "hata ayıkla", "program yaz", "yaz bir",
		},
	},
	{
		Name:        "concise",
		Description: "Answer briefly: no preamble, no restating the question, no filler.",
		System: "Answer as briefly as possible. No preamble, no restating the " +
			"question, no filler or apologies. Prefer a direct answer; add " +
			"detail only when it changes the outcome.",
		Triggers: []string{
			"briefly", "be brief", "short", "tldr", "one sentence", "concise",
			"in a nutshell", "kısaca", "kısa", "tek cümle", "özet",
		},
	},
	{
		Name:        "turkish",
		Description: "Always reply in Turkish, regardless of the prompt's language.",
		System:      "Always reply in Turkish (Türkçe), regardless of the language of the prompt.",
	},
	{
		Name:        "shell-safe",
		Description: "When giving shell commands, prefer safe, non-destructive forms.",
		System: "When you give shell commands, prefer safe, non-destructive " +
			"forms. Never suggest rm -rf on a broad path, and call out any " +
			"command that deletes or overwrites data.",
		Triggers: []string{
			"shell", "bash", "zsh", "terminal", "command", "chmod", "chown",
			"sudo", "docker", "kubectl", "grep", "awk", "sed", "script",
			"komut", "rm -", "curl", "find ",
		},
	},
}

// All returns every embedded skill, sorted by name.
func All() []Skill {
	out := make([]Skill, len(all))
	copy(out, all)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the skill with the given name.
func Get(name string) (Skill, bool) {
	for _, s := range all {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}

// Combine joins the system text of the named skills, in the order given, into a
// single system message. Unknown names are ignored; the caller is expected to
// have validated them so it can report the mistake.
func Combine(names []string) string {
	var parts []string
	for _, n := range names {
		if s, ok := Get(n); ok {
			parts = append(parts, s.System)
		}
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "\n\n" + p
	}
	return out
}

// Names returns the names of the named skills that exist, de-duplicated and in
// the order given.
func Names(names []string) []string {
	seen := make(map[string]bool, len(names))
	var out []string
	for _, n := range names {
		if _, ok := Get(n); ok && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// Auto returns the names of the skills that should be attached automatically to
// the given prompt: any skill whose trigger appears in the prompt, plus the
// turkish skill when the prompt itself is written in Turkish. The result is
// sorted by name so the combined system text is deterministic.
func Auto(prompt string) []string {
	lower := strings.ToLower(prompt)
	var out []string
	for _, s := range all {
		if s.Name == "turkish" {
			if looksTurkish(prompt) {
				out = append(out, s.Name)
			}
			continue
		}
		for _, t := range s.Triggers {
			if strings.Contains(lower, t) {
				out = append(out, s.Name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// turkishLetters are the characters that appear in Turkish but not in English.
const turkishLetters = "ğışçöüİĞŞÇÖÜ"

// turkishWords are common Turkish words used to detect the prompt's language.
var turkishWords = []string{
	"bir", "bu", "şu", "için", "ile", "nasıl", "neden", "nedir", "mi", "mı",
	"değil", "var", "yok", "çok", "daha", "ama", "ve", "veya", "ben", "sen",
	"bana", "yap", "yaz", "ver", "anlat", "açıkla", "nerede", "hangi", "lütfen",
	"merhaba", "selam", "teşekkür", "kodu", "kod", "hata",
}

// looksTurkish reports whether the prompt is likely written in Turkish, using
// Turkish-specific letters first and common Turkish words as a fallback.
func looksTurkish(prompt string) bool {
	lower := strings.ToLower(prompt)
	if strings.ContainsAny(lower, turkishLetters) {
		return true
	}
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, w := range words {
		for _, tw := range turkishWords {
			if w == tw {
				return true
			}
		}
	}
	return false
}
