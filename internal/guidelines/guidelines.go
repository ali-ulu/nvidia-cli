// Package guidelines embeds the lean-coding guidelines that nv can prepend to a
// prompt as a system message.
package guidelines

import (
	_ "embed"
	"strings"
)

//go:embed GUIDELINES.md
var text string

// Text returns the full guidelines document.
func Text() string { return text }

// SystemPrompt wraps the guidelines in the instruction that tells the model to
// follow them. It is used verbatim as the system message.
func SystemPrompt() string {
	return "Follow these engineering guidelines for any code you write:\n\n" +
		strings.TrimSpace(text)
}
