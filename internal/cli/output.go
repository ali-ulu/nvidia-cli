package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ali-ulu/nvidia-cli/internal/nim"
)

// PrintModels writes models as an aligned table, keeping only ids that contain
// filter (case-insensitive). An empty filter keeps everything.
func PrintModels(w io.Writer, models []nim.Model, filter string) {
	matched := filterModels(models, filter)
	if len(matched) == 0 {
		fmt.Fprintf(w, "no models matched %q\n", filter)
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tOWNER")
	for _, m := range matched {
		fmt.Fprintf(tw, "%s\t%s\n", m.ID, m.OwnedBy)
	}
	tw.Flush()
}

func filterModels(models []nim.Model, filter string) []nim.Model {
	if filter == "" {
		return models
	}
	needle := strings.ToLower(filter)
	var out []nim.Model
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.ID), needle) {
			out = append(out, m)
		}
	}
	return out
}
