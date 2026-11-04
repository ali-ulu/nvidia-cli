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
| `--api-key KEY` | all | API key; wins over `NVIDIA_API_KEY` |
| `--base-url URL` | all | override the endpoint (default `https://integrate.api.nvidia.com/v1`) |
| `--filter TEXT` | `models` | case-insensitive id filter |
| `--model NAME` | `ask` | model to use (required) |
| `--system TEXT` | `ask` | optional system prompt |
| `--guidelines` | `ask` | prepend the embedded coding guidelines to the system prompt |

`nv` exits `0` on success and `1` on any error, printing the reason to stderr.

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
internal/nim/            HTTP client for the NIM API
internal/cli/            API key resolution and output formatting
internal/guidelines/     embedded coding guidelines (go:embed)
```

## License

MIT. The embedded guidelines keep their upstream MIT attribution; see
`internal/guidelines/CREDITS.md`.
