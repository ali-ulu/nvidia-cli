// Command nv is a small CLI for the NVIDIA NIM inference API.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ali-ulu/nv/internal/cli"
	"github.com/ali-ulu/nv/internal/guidelines"
	"github.com/ali-ulu/nv/internal/nim"
)

const usage = `nv - talk to the NVIDIA NIM API

Usage:
  nv models [--filter TEXT]   list available models
  nv ask [flags] PROMPT       send a single prompt
  nv guidelines               print the embedded coding guidelines

Flags:
  --api-key KEY    API key (defaults to $NVIDIA_API_KEY)
  --base-url URL   API base URL (defaults to the hosted NIM endpoint)
  --model NAME     model to use for ask
  --system TEXT    system prompt for ask
  --guidelines     prepend the embedded coding guidelines to the system prompt
  --filter TEXT    only show models whose id contains TEXT (models)
`

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
		return runModels(args[1:], stdout, stderr)
	case "ask":
		return runAsk(args[1:], stdout, stderr)
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

func runModels(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("models", stderr)
	apiKey := fs.String("api-key", "", "API key")
	baseURL := fs.String("base-url", "", "API base URL")
	filter := fs.String("filter", "", "only show models whose id contains TEXT")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	key, err := cli.APIKey(*apiKey)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	models, err := nim.New(key, *baseURL).Models(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cli.PrintModels(stdout, models, *filter)
	return 0
}

func runAsk(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("ask", stderr)
	apiKey := fs.String("api-key", "", "API key")
	baseURL := fs.String("base-url", "", "API base URL")
	model := fs.String("model", "", "model to use")
	system := fs.String("system", "", "system prompt")
	useGuidelines := fs.Bool("guidelines", false, "prepend the embedded coding guidelines")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(stderr, "ask needs a prompt")
		return 1
	}
	if *model == "" {
		fmt.Fprintln(stderr, "ask needs --model")
		return 1
	}

	key, err := cli.APIKey(*apiKey)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	messages := make([]nim.Message, 0, 2)
	systemPrompt := *system
	if *useGuidelines {
		if systemPrompt != "" {
			systemPrompt = guidelines.SystemPrompt() + "\n\n" + systemPrompt
		} else {
			systemPrompt = guidelines.SystemPrompt()
		}
	}
	if systemPrompt != "" {
		messages = append(messages, nim.Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, nim.Message{Role: "user", Content: prompt})

	reply, err := nim.New(key, *baseURL).Chat(context.Background(), nim.ChatRequest{
		Model:    *model,
		Messages: messages,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, reply)
	return 0
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	return fs
}
