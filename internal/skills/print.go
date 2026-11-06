package skills

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// Print writes the embedded skills as an aligned table (name, description).
func Print(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SKILL\tDESCRIPTION")
	for _, s := range All() {
		fmt.Fprintf(tw, "%s\t%s\n", s.Name, s.Description)
	}
	tw.Flush()
}

// PrintJSON writes the embedded skills as indented JSON.
func PrintJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(All())
}
