# nv

A small CLI for the [NVIDIA NIM](https://build.nvidia.com) inference API.

NIM speaks the OpenAI wire protocol, so `nv` talks to two endpoints:
`GET /models` and `POST /chat/completions`.

It also ships an embedded set of lean-coding guidelines you can hand to the model.

## Install

```sh
go install github.com/ali-ulu/nvidia-cli@latest
```

Or build locally:

```sh
make build   # produces ./nv
```

## Setup

Get a key from [build.nvidia.com](https://build.nvidia.com) (it starts with `nvapi-`)
and export it:

```sh
export NVIDIA_API_KEY="nvapi-..."
```

## Usage

```sh
# List every model (served from a cache; refreshed on a TTL)
nv models

# Force a network refresh and update the cache
nv models --refresh

# Only models whose id contains "nemotron"
nv models --filter nemotron

# Use the cached list only; never call the API
nv models --offline

# Send a single prompt
nv ask --model nvidia/nemotron-3-super-120b-a12b "Explain what NIM is in one sentence."

# The model is optional: with no --model and no config it falls back to a built-in default
nv ask "Explain what NIM is in one sentence."

# Stream the reply token by token
nv ask --model nvidia/nemotron-3-super-120b-a12b --stream "Tell me a haiku about GPUs."

# Multi-turn chat session (commands: :model NAME, :reset, :quit)
nv repl --model nvidia/nemotron-3-super-120b-a12b

# Store the key and default model once (writes ~/.config/nv/config.json, mode 0600)
nv config --api-key nvapi-... --model nvidia/nemotron-3-super-120b-a12b

# Now the model is remembered
nv ask "Explain what NIM is in one sentence."

# Add a system prompt
nv ask --model nvidia/nemotron-3-super-120b-a12b \
  --system "Answer in Turkish." "Merhaba"

# Attach one or more embedded skills (repeatable)
nv ask --model nvidia/nemotron-3-super-120b-a12b \
  --skill lean-coding --skill concise "Write a retry helper in Go."

# List the embedded skills (or --json for machine-readable output)
nv skills

# Read the guidelines themselves
nv guidelines
```

### Flags

| Flag | Applies to | Meaning |
|---|---|---|
| `--api-key KEY` | all | API key; priority: flag > `$NVIDIA_API_KEY` > config file |
| `--base-url URL` | all | override the endpoint (default `https://integrate.api.nvidia.com/v1`) |
| `--filter TEXT` | `models` | case-insensitive id filter |
| `--refresh` | `models` | force a network fetch and update the model cache |
| `--offline` | `models` | use the cached model list only; never call the API |
| `--model NAME` | `ask`, `repl` | model to use (falls back to the config file, then the built-in default) |
| `--stream` | `ask` | stream the reply via SSE as tokens arrive |
| `--system TEXT` | `ask`, `repl` | optional system prompt |
| `--skill NAME` | `ask`, `repl` | attach an embedded skill's system prompt (repeatable) |
| `--guidelines` | `ask`, `repl` | attach the embedded coding guidelines (alias of `--skill lean-coding`) |
| `--json` | `skills` | emit the skill list as JSON |

`nv` exits `0` on success and `1` on any error, printing the reason to stderr.

### Model list cache

`nv models` caches the API's list on disk so repeated calls do not hit the
network. The cache lives at `~/.cache/nv/models.json` (`XDG_CACHE_HOME` is
honored) and is refreshed when it is older than 24 hours. `--refresh` forces a
fetch; `--offline` serves the cache without any network call. If a fetch fails
while a cached list exists, the cached list is used with a warning on stderr
rather than failing.

### Skills

A skill is a named, embedded system prompt that `nv ask` and `nv repl` can
attach with `--skill NAME` (repeatable). `nv skills` lists them and
`nv skills --json` prints them as JSON. The embedded skills are:

| Skill | Effect |
|---|---|
| `lean-coding` | the embedded lean-coding guidelines (also `--guidelines`) |
| `concise` | answer briefly, without preamble or filler |
| `turkish` | always reply in Turkish |
| `shell-safe` | prefer safe, non-destructive shell commands |

An attached skill's text is prepended to the user's own `--system` text, so an
explicit instruction always wins.

### Config file

`nv config` reads and `nv config --api-key/--base-url/--model` writes
`~/.config/nv/config.json` (`XDG_CONFIG_HOME` is honored). The file is created
with mode `0600` and may hold `api_key`, `base_url` and `model`; explicit flags
and the environment always win over it.

### Retries

Requests that fail with `429` or a `5xx` status, or at the transport layer, are
retried up to 4 times with exponential backoff and jitter; a `Retry-After`
header (seconds or HTTP date) is honored. Client errors such as `401`, `403`,
`404` and `410` are never retried.

## Development

```sh
make check   # gofmt + go vet + go test -race
make lint    # golangci-lint
```

Tests run against a real local HTTP server (`net/http/httptest`); there are no mocks.

## Embedded guidelines

`nv guidelines` prints the coding guidelines embedded in the binary, and
`nv ask --guidelines` sends them as the system message. They merge two
MIT-licensed sources:

- [Karpathy Guidelines](https://github.com/multica-ai/andrej-karpathy-skills) by forrestchang
- [Ponytail](https://github.com/dietrichgebert/ponytail) by DietrichGebert

The merged text lives in `internal/guidelines/GUIDELINES.md` and the attribution
in `internal/guidelines/CREDITS.md`.

## Layout

```
main.go                  command dispatch and flags
internal/nim/            HTTP client for the NIM API (retry, SSE streaming)
internal/cli/            config file, model cache, API key resolution, REPL, output
internal/skills/         embedded named skills (system prompts)
internal/guidelines/     embedded coding guidelines (go:embed)
```

## License

MIT. The embedded guidelines keep their upstream MIT attribution; see
`internal/guidelines/CREDITS.md`.
