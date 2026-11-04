package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ali-ulu/nvidia-cli/internal/guidelines"
	"github.com/ali-ulu/nvidia-cli/internal/nim"
)

// REPL commands.
const (
	cmdModel = ":model"
	cmdReset = ":reset"
	cmdQuit  = ":quit"
)

// REPL is a multi-turn chat session. stdin and stdout are injected so the
// loop can be driven from a test.
type REPL struct {
	client *nim.Client
	out    io.Writer
	// Model is the chat model the session starts with.
	Model string
	// System, when set, is sent as the first message of every turn.
	System string
	// UseGuidelines prepends the embedded lean-coding guidelines to System.
	UseGuidelines bool
}

// NewREPL returns a session that talks through client and prints to out.
func NewREPL(client *nim.Client, out io.Writer, model string) *REPL {
	return &REPL{client: client, out: out, Model: model}
}

// Run reads lines from in until :quit or EOF. Each non-command line is sent
// as a user turn and the reply is streamed to out. It returns the process
// exit code (0 on a clean exit).
func (r *REPL) Run(in io.Reader) int {
	var history []nim.Message
	if sys := r.systemPrompt(); sys != "" {
		history = append(history, nim.Message{Role: "system", Content: sys})
	}

	fmt.Fprintf(r.out, "nv repl — model %s; commands: :model NAME, :reset, :quit\n", r.Model)
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		switch {
		case line == cmdQuit || line == ":exit":
			return 0
		case line == cmdReset:
			history = nil
			if sys := r.systemPrompt(); sys != "" {
				history = append(history, nim.Message{Role: "system", Content: sys})
			}
			fmt.Fprintln(r.out, "history cleared")
		case strings.HasPrefix(line, cmdModel+" "):
			name := strings.TrimSpace(strings.TrimPrefix(line, cmdModel))
			if name == "" {
				fmt.Fprintln(r.out, "usage: :model NAME")
			} else {
				r.Model = name
				fmt.Fprintf(r.out, "model set to %s\n", name)
			}
		case strings.HasPrefix(line, ":"):
			fmt.Fprintln(r.out, "unknown command; try :model NAME, :reset, :quit")
		default:
			history = append(history, nim.Message{Role: "user", Content: line})
			reply, err := r.client.ChatStream(context.Background(), nim.ChatRequest{
				Model:    r.Model,
				Messages: history,
			}, func(delta string) error {
				_, werr := io.WriteString(r.out, delta)
				return werr
			})
			fmt.Fprintln(r.out)
			if err != nil {
				fmt.Fprintln(r.out, "error:", err)
				history = history[:len(history)-1] // failed turn is not kept
				continue
			}
			history = append(history, nim.Message{Role: "assistant", Content: reply})
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(r.out, "read error:", err)
		return 1
	}
	return 0
}

// systemPrompt assembles the configured system message, if any.
func (r *REPL) systemPrompt() string {
	if r.UseGuidelines {
		g := guidelines.Text()
		if r.System != "" {
			return g + "\n\n" + r.System
		}
		return g
	}
	return r.System
}
