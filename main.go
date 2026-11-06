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
	"github.com/ali-ulu/nvidia-cli/internal/skills"
)

const usage = `nv - talk to the NVIDIA NIM API

Usage:
  nv models [--filter TEXT] [--refresh] [--offline]
                              list available models (cached, refreshed on a TTL)
  nv ask [flags] PROMPT       send a single prompt
  nv repl [flags]             multi-turn chat session
  nv config [flags]           read or write ~/.config/nv/config.json
  nv skills [--json]          list the embedded skills
  nv guidelines               print the embedded coding guidelines

Flags:
  --api-key KEY    API key (priority: flag > $NVIDIA_API_KEY > config file)
  --base-url URL   API base URL (defaults to the hosted NIM endpoint)
  --model NAME     model to use for ask/repl (defaults to the config file, then
                   to the built-in default)
  --stream         stream the reply token by token (ask)
  --system TEXT    system prompt for ask/repl
  --skill NAME     attach an embedded skill's system prompt (repeatable)
  --guidelines     attach the embedded coding guidelines (alias of --skill lean-coding)
  --filter TEXT    only show models whose id contains TEXT (models)
  --refresh        force a network fetch and update the model cache (models)
  --offline        use the cached model list only; never call the API (models)
  --json           emit the skill list as JSON (skills)

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
	case "skills":
		return runSkills(args[1:], stdout, stderr)
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

// resolveModel picks the model for ask/repl, in priority order:
// --model flag > config file > the built-in default. It only errors when the
// config file cannot be read.
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
	return nim.DefaultModel, nil
}

func runModels(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("models", stderr)
	apiKey := fs.String("api-key", "", "API key")
	baseURL := fs.String("base-url", "", "API base URL")
	filter := fs.String("filter", "", "only show models whose id contains TEXT")
	refresh := fs.Bool("refresh", false, "force a network fetch and update the cache")
	offline := fs.Bool("offline", false, "use the cached list only; never call the API")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cached, hasCache, err := cli.LoadModelsCache()
	if err != nil {
		// A corrupt cache must not break the command: warn and fall through.
		fmt.Fprintln(stderr, "warning:", err)
		hasCache = false
	}

	// Offline mode: serve whatever is cached and never touch the network.
	if *offline {
		if !hasCache {
			fmt.Fprintln(stderr, "no cached model list; run nv models once while online")
			return 1
		}
		cli.PrintModels(stdout, cached.Models, *filter)
		return 0
	}

	// Fresh cache and no --refresh: serve it without a network round trip.
	if hasCache && !*refresh && cached.Fresh(time.Now()) {
		cli.PrintModels(stdout, cached.Models, *filter)
		return 0
	}

	client, err := connection(*apiKey, *baseURL)
	if err != nil {
		// Fall back to a stale cache rather than failing outright.
		if hasCache {
			fmt.Fprintln(stderr, "warning:", err, "(using cached list)")
			cli.PrintModels(stdout, cached.Models, *filter)
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 1
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	models, err := client.Models(reqCtx)
	if err != nil {
		if hasCache {
			fmt.Fprintln(stderr, "warning:", err, "(using cached list)")
			cli.PrintModels(stdout, cached.Models, *filter)
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 1
	}

	// Refresh the cache; a write failure is a warning, not a command failure.
	if err := cli.SaveModelsCache(cli.ModelsCache{FetchedAt: time.Now(), Models: models}); err != nil {
		fmt.Fprintln(stderr, "warning:", err)
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
	skillsFlag := multiFlag{}
	fs.Var(&skillsFlag, "skill", "attach an embedded skill (repeatable)")
	stream := fs.Bool("stream", false, "stream the reply token by token")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(stderr, "ask needs a prompt")
		return 1
	}
	names, err := resolveSkills(skillsFlag, *useGuidelines)
	if err != nil {
		fmt.Fprintln(stderr, err)
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

	req := nim.ChatRequest{Model: name, Messages: messages(*system, names, prompt)}
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

// messages builds the message list for one ask turn. The attached skills'
// system text is prepended ahead of the user's own --system text so an explicit
// instruction always wins over a skill.
func messages(system string, skillNames []string, prompt string) []nim.Message {
	msgs := make([]nim.Message, 0, 2)
	if combined := skills.Combine(skillNames); combined != "" {
		if system != "" {
			system = combined + "\n\n" + system
		} else {
			system = combined
		}
	}
	if system != "" {
		msgs = append(msgs, nim.Message{Role: "system", Content: system})
	}
	return append(msgs, nim.Message{Role: "user", Content: prompt})
}

// multiFlag collects a repeatable string flag (--skill a --skill b).
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// resolveSkills validates the --skill names and folds in --guidelines as an
// alias for the lean-coding skill. It returns the names to attach, in order.
func resolveSkills(skillFlag multiFlag, useGuidelines bool) ([]string, error) {
	requested := []string(skillFlag)
	if useGuidelines {
		requested = append([]string{"lean-coding"}, requested...)
	}
	for _, n := range requested {
		if _, ok := skills.Get(n); !ok {
			return nil, fmt.Errorf("unknown skill %q; run nv skills to list them", n)
		}
	}
	return skills.Names(requested), nil
}

// runSkills lists the embedded skills as a table or, with --json, as JSON.
func runSkills(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("skills", stderr)
	asJSON := fs.Bool("json", false, "emit the list as JSON")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *asJSON {
		if err := skills.PrintJSON(stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	skills.Print(stdout)
	return 0
}

func runRepl(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("repl", stderr)
	apiKey := fs.String("api-key", "", "API key")
	baseURL := fs.String("base-url", "", "API base URL")
	model := fs.String("model", "", "model to use")
	system := fs.String("system", "", "system prompt")
	useGuidelines := fs.Bool("guidelines", false, "prepend the embedded coding guidelines")
	skillsFlag := multiFlag{}
	fs.Var(&skillsFlag, "skill", "attach an embedded skill (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	names, err := resolveSkills(skillsFlag, *useGuidelines)
	if err != nil {
		fmt.Fprintln(stderr, err)
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
	session.Skills = names
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
