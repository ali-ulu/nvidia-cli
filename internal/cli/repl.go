package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/ali-ulu/nvidia-cli/internal/nim"
	"github.com/ali-ulu/nvidia-cli/internal/skills"
)

// REPL commands.
const (
	cmdModel = ":model"
	cmdReset = ":reset"
	cmdQuit  = ":quit"
)

// History budget: the request payload is trimmed to a rough token estimate
// (4 chars per token) so long sessions do not send an unbounded history.
const (
	charsPerToken    = 4
	maxHistoryTokens = 8000
)

// maxLineBytes is the per-line stdin limit (the bufio.Scanner default is 64 KiB).
const maxLineBytes = 1 << 20 // 1 MiB

// REPL is a multi-turn chat session. stdin and stdout are injected so the
// loop can be driven from a test.
type REPL struct {
	client *nim.Client
	out    io.Writer
	// Model is the chat model the session starts with.
	Model string
	// System, when set, is sent as the first message of every turn.
	System string
	// Skills are the names of embedded skills whose system text is prepended
	// to System.
	Skills []string
}

// NewREPL returns a session that talks through client and prints to out.
func NewREPL(client *nim.Client, out io.Writer, model string) *REPL {
	return &REPL{client: client, out: out, Model: model}
}

// Run reads lines from in until :quit or EOF. Each non-command line is sent
// as a user turn and the reply is streamed to out. SIGINT during a stream
// cancels only the current turn (the turn is dropped and the prompt returns);
// at the prompt SIGINT keeps its default terminate-the-process behaviour
// because the handler is re-armed per turn and released right after. It
// returns the process exit code (0 on a clean exit).
func (r *REPL) Run(ctx context.Context, in io.Reader) int {
	var history []nim.Message
	if sys := r.systemPrompt(); sys != "" {
		history = append(history, nim.Message{Role: "system", Content: sys})
	}

	fmt.Fprintf(r.out, "nv repl — model %s; commands: :model NAME, :reset, :quit\n", r.Model)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
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
			var dropped int
			history, dropped = trimHistory(history, maxHistoryTokens)
			if dropped > 0 {
				fmt.Fprintf(r.out, "note: trimmed %d oldest turn(s) to fit the context budget\n", dropped/2)
			}
			// A fresh signal context per turn: Ctrl+C aborts the current
			// generation, and the handler is gone again by the next prompt.
			turnCtx, stopTurn := signal.NotifyContext(ctx, os.Interrupt)
			reply, err := r.client.ChatStream(turnCtx, nim.ChatRequest{
				Model:    r.Model,
				Messages: history,
			}, func(delta string) error {
				_, werr := io.WriteString(r.out, delta)
				return werr
			})
			// Check before stop: NotifyContext's stop cancels the context,
			// so Err() is only meaningful while the handler is live.
			interrupted := turnCtx.Err() != nil
			stopTurn()
			if interrupted {
				fmt.Fprintln(r.out, "^C")
				history = history[:len(history)-1] // interrupted turn is not kept
				continue
			}
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

// systemPrompt assembles the configured system message, if any: the attached
// skills' text first, then the user's own --system text.
func (r *REPL) systemPrompt() string {
	system := r.System
	if combined := skills.Combine(r.Skills); combined != "" {
		if system != "" {
			system = combined + "\n\n" + system
		} else {
			system = combined
		}
	}
	return system
}

// tokenEstimate gives a coarse token count for a message (~4 chars/token).
func tokenEstimate(m nim.Message) int {
	return (len(m.Content) + charsPerToken - 1) / charsPerToken
}

// trimHistory drops the oldest complete user+assistant turns (after any
// leading system message) until the estimated token count fits maxTokens.
// Dropping whole pairs keeps the wire history well formed: the model never
// sees an assistant reply whose prompting user turn is gone. The system
// message and the newest turn are never dropped, so a single oversized turn
// still goes through.
func trimHistory(msgs []nim.Message, maxTokens int) ([]nim.Message, int) {
	total := 0
	for _, m := range msgs {
		total += tokenEstimate(m)
	}
	if total <= maxTokens {
		return msgs, 0
	}
	sys := 0
	for sys < len(msgs) && msgs[sys].Role == "system" {
		sys++
	}
	dropped := 0
	for total > maxTokens && len(msgs)-sys-dropped > 1 &&
		msgs[sys+dropped].Role == "user" && msgs[sys+dropped+1].Role == "assistant" {
		total -= tokenEstimate(msgs[sys+dropped]) + tokenEstimate(msgs[sys+dropped+1])
		dropped += 2
	}
	// Re-attach the system prefix; the dropped turns are removed from the rest.
	trimmed := make([]nim.Message, 0, sys+len(msgs)-sys-dropped)
	trimmed = append(trimmed, msgs[:sys]...)
	trimmed = append(trimmed, msgs[sys+dropped:]...)
	return trimmed, dropped
}
