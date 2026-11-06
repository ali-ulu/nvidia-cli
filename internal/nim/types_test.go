package nim

import (
	"strings"
	"testing"
)

// TestAPIErrorErrorRendersStatusAndBody covers the three shapes Error() can
// take: a normal body, an empty body, and a body long enough to be truncated.
func TestAPIErrorErrorRendersStatusAndBody(t *testing.T) {
	t.Run("body surfaced", func(t *testing.T) {
		err := &APIError{Status: 401, Body: "  invalid key  "}
		if got, want := err.Error(), "api returned HTTP 401: invalid key"; got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		err := &APIError{Status: 500, Body: "   "}
		if got, want := err.Error(), "api returned HTTP 500"; got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("long body truncated", func(t *testing.T) {
		err := &APIError{Status: 400, Body: strings.Repeat("x", 400)}
		got := err.Error()
		if !strings.HasSuffix(got, "...") {
			t.Errorf("Error() = %q, want a truncated body ending in ...", got)
		}
		// 300 body chars + "..." plus the "api returned HTTP 400: " prefix.
		if want := len("api returned HTTP 400: ") + 300 + 3; len(got) != want {
			t.Errorf("Error() length = %d, want %d", len(got), want)
		}
	})
}
