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
# List every model
nv models

# Only models whose id contains "nemotron"
nv models --filter nemotron

# Send a single prompt
nv ask --model nvidia/nemotron-3-super-120b-a12b "Explain what NIM is in one sentence."

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

# Ask for code with the embedded lean-coding guidelines attached
nv ask --model nvidia/nemotron-3-super-120b-a12b \
  --guidelines "Write a retry helper in Go."

# Read the guidelines themselves
nv guidelines
```

### Flags

| Flag | Applies to | Meaning |
|---|---|---|
| `--api-key KEY` | all | API key; priority: flag > `$NVIDIA_API_KEY` > config file |
| `--base-url URL` | all | override the endpoint (default `https://integrate.api.nvidia.com/v1`) |
| `--filter TEXT` | `models` | case-insensitive id filter |
| `--model NAME` | `ask`, `repl` | model to use (falls back to the config file) |
| `--stream` | `ask` | stream the reply via SSE as tokens arrive |
| `--system TEXT` | `ask`, `repl` | optional system prompt |
| `--guidelines` | `ask`, `repl` | prepend the embedded coding guidelines to the system prompt |

`nv` exits `0` on success and `1` on any error, printing the reason to stderr.

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
internal/cli/            config file, API key resolution, REPL, output formatting
internal/guidelines/     embedded coding guidelines (go:embed)
```

## License

MIT. The embedded guidelines keep their upstream MIT attribution; see
`internal/guidelines/CREDITS.md`.
