// Command nv is a small CLI for the NVIDIA NIM inference API.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ali-ulu/nvidia-cli/internal/cli"
	"github.com/ali-ulu/nvidia-cli/internal/guidelines"
	"github.com/ali-ulu/nvidia-cli/internal/nim"
)

const usage = `nv - talk to the NVIDIA NIM API

Usage:
  nv models [--filter TEXT]   list available models
  nv ask [flags] PROMPT       send a single prompt
  nv repl [flags]             multi-turn chat session
  nv config [flags]           read or write ~/.config/nv/config.json
  nv guidelines               print the embedded coding guidelines

Flags:
  --api-key KEY    API key (priority: flag > $NVIDIA_API_KEY > config file)
  --base-url URL   API base URL (defaults to the hosted NIM endpoint)
  --model NAME     model to use for ask/repl
  --stream         stream the reply token by token (ask)
  --system TEXT    system prompt for ask/repl
  --guidelines     prepend the embedded coding guidelines to the system prompt
  --filter TEXT    only show models whose id contains TEXT (models)

Config commands:
  nv config                                            print the config file
  nv config --api-key KEY --model NAME --base-url URL  write the given fields
`

// stdin is the reader the repl command consumes; a variable so tests can
// inject input without a real os.File.
var stdin io.Reader = os.Stdin

// requestTimeout bounds each non-streaming models/ask API call; a variable so
// tests can shorten it instead of waiting out the default. Streaming calls
// deliberately run without it (see runAsk).
var requestTimeout = 2 * time.Minute

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point: it never touches os.Args or os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}

	switch args[0] {
	case "models":
		// Ctrl+C cancels the in-flight request through the signal context.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return runModels(ctx, args[1:], stdout, stderr)
	case "ask":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return runAsk(ctx, args[1:], stdout, stderr)
	case "repl":
		// No process-wide SIGINT handler: the REPL re-arms the signal per
		// turn (see cli.REPL.Run), and Ctrl+C at the prompt terminates nv
		// with its default handling instead of being swallowed.
		return runRepl(context.Background(), args[1:], stdout, stderr)
	case "config":
		return runConfig(args[1:], stdout, stderr)
	case "guidelines":
		fmt.Fprintln(stdout, guidelines.Text())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 1
	}
}

// connection resolves the API key with the flag > env > config-file priority
// and returns a client pointed at the configured base URL.
func connection(flagKey, flagBaseURL string) (*nim.Client, error) {
	cfg, err := cli.LoadConfig()
	if err != nil {
		return nil, err
	}
	key, err := cli.APIKey(flagKey, cfg)
	if err != nil {
		return nil, err
	}
	if flagBaseURL == "" {
		flagBaseURL = cfg.BaseURL
	}
	return nim.New(key, flagBaseURL), nil
}

// resolveModel applies the config-file model as the default for --model.
func resolveModel(flagModel string) (string, error) {
	if flagModel != "" {
		return flagModel, nil
	}
	cfg, err := cli.LoadConfig()
	if err != nil {
		return "", err
	}
	if cfg.Model != "" {
		return cfg.Model, nil
	}
	return "", fmt.Errorf("no model: pass --model or run nv config --model NAME")
}

func runModels(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("models", stderr)
	apiKey := fs.String("api-key", "", "API key")
	baseURL := fs.String("base-url", "", "API base URL")
	filter := fs.String("filter", "", "only show models whose id contains TEXT")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	client, err := connection(*apiKey, *baseURL)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	models, err := client.Models(reqCtx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cli.PrintModels(stdout, models, *filter)
	return 0
}

func runAsk(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("ask", stderr)
	apiKey := fs.String("api-key", "", "API key")
	baseURL := fs.String("base-url", "", "API base URL")
	model := fs.String("model", "", "model to use")
	system := fs.String("system", "", "system prompt")
	useGuidelines := fs.Bool("guidelines", false, "prepend the embedded coding guidelines")
	stream := fs.Bool("stream", false, "stream the reply token by token")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(stderr, "ask needs a prompt")
		return 1
	}
	name, err := resolveModel(*model)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	client, err := connection(*apiKey, *baseURL)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	req := nim.ChatRequest{Model: name, Messages: messages(*system, *useGuidelines, prompt)}
	if *stream {
		// The stream deliberately runs on the bare signal context: a
		// whole-stream deadline would cut a long generation short after
		// partial deltas are already on stdout. Ctrl+C still aborts it.
		if _, err := client.ChatStream(ctx, req, func(delta string) error {
			_, werr := io.WriteString(stdout, delta)
			return werr
		}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout)
		return 0
	}
	// The deadline rides on the per-request context; the http.Client itself
	// stays without a Timeout so it never cuts a long-running stream short.
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	reply, err := client.Chat(reqCtx, req)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, reply)
	return 0
}

// messages builds the message list for one ask turn.
func messages(system string, useGuidelines bool, prompt string) []nim.Message {
	msgs := make([]nim.Message, 0, 2)
	if useGuidelines {
		if system != "" {
			system = guidelines.SystemPrompt() + "\n\n" + system
		} else {
			system = guidelines.SystemPrompt()
		}
	}
	if system != "" {
		msgs = append(msgs, nim.Message{Role: "system", Content: system})
	}
	return append(msgs, nim.Message{Role: "user", Content: prompt})
}

func runRepl(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("repl", stderr)
	apiKey := fs.String("api-key", "", "API key")
	baseURL := fs.String("base-url", "", "API base URL")
	model := fs.String("model", "", "model to use")
	system := fs.String("system", "", "system prompt")
	useGuidelines := fs.Bool("guidelines", false, "prepend the embedded coding guidelines")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	name, err := resolveModel(*model)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	client, err := connection(*apiKey, *baseURL)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	session := cli.NewREPL(client, stdout, name)
	session.System = *system
	session.UseGuidelines = *useGuidelines
	return session.Run(ctx, stdin)
}

func runConfig(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("config", stderr)
	apiKey := fs.String("api-key", "", "API key to store")
	baseURL := fs.String("base-url", "", "API base URL to store")
	model := fs.String("model", "", "default model to store")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := cli.LoadConfig()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *apiKey == "" && *baseURL == "" && *model == "" {
		path, err := cli.ConfigPath()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "config file: %s\n%s\n", path, marshalConfig(cfg))
		return 0
	}
	if *apiKey != "" {
		cfg.APIKey = *apiKey
	}
	if *baseURL != "" {
		cfg.BaseURL = *baseURL
	}
	if *model != "" {
		cfg.Model = *model
	}
	if err := cli.SaveConfig(cfg); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, marshalConfig(cfg))
	return 0
}

// marshalConfig renders cfg as indented JSON for display, with the API key
// masked so the full secret never reaches the terminal. HTML escaping is off
// so the mask markers print literally.
func marshalConfig(cfg cli.Config) string {
	cfg.APIKey = maskKey(cfg.APIKey)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}

// maskKey reveals only the first 6 and last 4 characters of a secret, hiding
// the middle. Keys too short to keep any of the middle hidden, and empty
// values, are fully masked.
func maskKey(key string) string {
	switch {
	case key == "":
		return "<yok>"
	case len(key) <= 10:
		return "****"
	default:
		return key[:6] + "****" + key[len(key)-4:]
	}
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	return fs
}
