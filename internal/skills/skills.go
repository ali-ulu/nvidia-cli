// Package skills holds the named system prompts (skills) embedded in the nv
// binary. A skill is a reusable instruction block that ask/repl can attach to
// a request with --skill NAME, independently of the user's own --system text.
package skills

import (
	"sort"

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
}

// all is the registry of embedded skills. Order here is the order they are
// combined in when several are attached to one request.
var all = []Skill{
	{
		Name:        "lean-coding",
		Description: "Lean-coding discipline for code answers (the embedded guidelines).",
		System:      guidelines.SystemPrompt(),
	},
	{
		Name:        "concise",
		Description: "Answer briefly: no preamble, no restating the question, no filler.",
		System: "Answer as briefly as possible. No preamble, no restating the " +
			"question, no filler or apologies. Prefer a direct answer; add " +
			"detail only when it changes the outcome.",
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
