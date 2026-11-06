package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ioPipe returns a ReadCloser and its write end, for faking stdin.
func ioPipe() (io.ReadCloser, io.WriteCloser) {
	pr, pw := io.Pipe()
	return pr, pw
}

// serve starts a real HTTP server and returns its base URL.
func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestModelsCommandEndToEnd(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(`{"data":[{"id":"nvidia/nemotron","owned_by":"nvidia"}]}`))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"models", "--base-url", base, "--api-key", "k"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nvidia/nemotron") {
		t.Errorf("stdout missing model:\n%s", stdout.String())
	}
}

func TestAskCommandEndToEnd(t *testing.T) {
	var gotBody string
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		gotBody = buf.String()
		w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"ask", "--base-url", base, "--api-key", "k",
		"--model", "nvidia/nemotron", "--system", "be brief", "ping",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "pong" {
		t.Errorf("stdout = %q, want pong", stdout.String())
	}
	for _, want := range []string{"be brief", "ping", "nvidia/nemotron"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %q: %s", want, gotBody)
		}
	}
}

func TestAskRequiresPromptAndModel(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no prompt", []string{"ask", "--model", "m"}, "needs a prompt"},
		{"no model", []string{"ask", "hello"}, "no model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate from any real config
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			if code == 0 {
				t.Fatalf("want non-zero exit")
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestMissingAPIKeyFails(t *testing.T) {
	t.Setenv("NVIDIA_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code := run([]string{"models"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("want non-zero exit without an API key")
	}
	if !strings.Contains(stderr.String(), "NVIDIA_API_KEY") {
		t.Errorf("stderr should name the env var: %q", stderr.String())
	}
}

func TestUnknownCommandFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"frobnicate"}, &stdout, &stderr); code == 0 {
		t.Fatal("want non-zero exit for an unknown command")
	}
}

func TestAPIErrorSurfacesToStderr(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key"}`))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"models", "--base-url", base, "--api-key", "bad"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("want non-zero exit on 401")
	}
	if !strings.Contains(stderr.String(), "401") {
		t.Errorf("stderr should mention the status: %q", stderr.String())
	}
}

func TestGuidelinesCommandPrintsDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"guidelines"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Think before coding", "shortcut:"} {
		if !strings.Contains(out, want) {
			t.Errorf("guidelines output missing %q", want)
		}
	}
}

func TestAskGuidelinesFlagPrependsSystemPrompt(t *testing.T) {
	var gotBody string
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		gotBody = buf.String()
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"ask", "--base-url", base, "--api-key", "k",
		"--model", "m", "--guidelines", "--system", "be brief", "ping",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}

	var sent struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("decode sent body: %v (%s)", err, gotBody)
	}
	if len(sent.Messages) != 2 {
		t.Fatalf("want system+user, got %d messages", len(sent.Messages))
	}
	if sent.Messages[0].Role != "system" {
		t.Errorf("first message role = %q, want system", sent.Messages[0].Role)
	}
	if !strings.Contains(sent.Messages[0].Content, "engineering guidelines") {
		t.Error("system message does not carry the guidelines")
	}
	if !strings.Contains(sent.Messages[0].Content, "be brief") {
		t.Error("--system text was dropped when combined with --guidelines")
	}
	if sent.Messages[1].Content != "ping" {
		t.Errorf("user message = %q, want ping", sent.Messages[1].Content)
	}
}

func TestAskWithoutGuidelinesSendsOnlyUserMessage(t *testing.T) {
	var gotBody string
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		gotBody = buf.String()
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"ask", "--base-url", base, "--api-key", "k", "--model", "m", "ping"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if strings.Contains(gotBody, "engineering guidelines") {
		t.Error("guidelines leaked into a request that did not ask for them")
	}
}

func TestAskStreamCommandEndToEnd(t *testing.T) {
	var gotAccept, gotBody string
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		gotBody = buf.String()
		// Deliberately split one SSE event across writes, mid-line.
		w.Write([]byte(`data: {"choices":[{"del`))
		w.Write([]byte(`ta":{"content":"to`))
		w.Write([]byte(`kened"}}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"ask", "--base-url", base, "--api-key", "k",
		"--model", "m", "--stream", "ping",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "tokened" {
		t.Errorf("stdout = %q, want tokened", stdout.String())
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", gotAccept)
	}
	if !strings.Contains(gotBody, `"stream":true`) {
		t.Errorf("request body missing stream:true: %s", gotBody)
	}
}

func TestReplCommandEndToEnd(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n"))
	})

	r, w := ioPipe()
	go func() {
		_, _ = w.Write([]byte("ping\n:quit\n"))
		w.Close()
	}()

	var stdout, stderr bytes.Buffer
	oldStdin := stdin
	stdin = r
	t.Cleanup(func() { stdin = oldStdin })

	code := run([]string{"repl", "--base-url", base, "--api-key", "k", "--model", "m"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pong") {
		t.Errorf("stdout missing streamed reply:\n%s", stdout.String())
	}
}

func TestConfigCommandWritesAndReads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	if code := run([]string{"config", "--api-key", testValue("stored"), "--model", "m1"}, &stdout, &stderr); code != 0 {
		t.Fatalf("write exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), maskKey(testValue("stored"))) || !strings.Contains(stdout.String(), "m1") {
		t.Errorf("write output missing masked fields:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), testValue("stored")) {
		t.Errorf("write output leaked the full API key:\n%s", stdout.String())
	}

	stdout.Reset()
	if code := run([]string{"config"}, &stdout, &stderr); code != 0 {
		t.Fatalf("read exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "config file:") || !strings.Contains(stdout.String(), "m1") {
		t.Errorf("read output missing stored config:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), maskKey(testValue("stored"))) {
		t.Errorf("read output missing masked API key:\n%s", stdout.String())
	}
}

// TestConfigOutputNeverPrintsFullAPIKey runs the config command in read and
// write mode and asserts the full key value never appears on the terminal.
func TestConfigOutputNeverPrintsFullAPIKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	key := testValue("leak-check") // 21 chars, long enough for head+tail masking

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"write", []string{"config", "--api-key", key, "--model", "m1"}},
		{"read", []string{"config"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != 0 {
				t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
			}
			out := stdout.String()
			if strings.Contains(out, key) {
				t.Errorf("full API key leaked to stdout:\n%s", out)
			}
			if !strings.Contains(out, maskKey(key)) {
				t.Errorf("output missing masked API key:\n%s", out)
			}
		})
	}
}

// TestMaskKey covers the masking helper directly, including the empty value
// marker and keys too short to reveal head and tail safely.
func TestMaskKey(t *testing.T) {
	key := testValue("maskme") // "test-value-maskme"
	want := key[:6] + "****" + key[len(key)-4:]
	if got := maskKey(key); got != want {
		t.Errorf("maskKey = %q, want %q", got, want)
	}
	if got := maskKey(""); got != "<yok>" {
		t.Errorf("maskKey(\"\") = %q, want <yok>", got)
	}
	if got := maskKey("shortkey12"); got != "****" {
		t.Errorf("maskKey(short) = %q, want full mask", got)
	}
}

func TestConfigReadShowsMarkerWhenKeyEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no config file, no key

	var stdout, stderr bytes.Buffer
	if code := run([]string{"config"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "<yok>") {
		t.Errorf("empty API key not marked in output:\n%s", stdout.String())
	}
}

func TestConfigFileModelUsedAsDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgPath := filepath.Join(dir, "nv", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"api_key":"`+testValue("file")+`","model":"key-file-model"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		if !strings.Contains(buf.String(), "key-file-model") {
			t.Errorf("config model not applied: %s", buf.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testValue("file") {
			t.Errorf("config api key not applied: %q", got)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"ask", "--base-url", base, "ping"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
}

// TestAskRequestTimesOut points ask at a server that stalls longer than the
// (shortened) request deadline and expects a failed exit that names the
// deadline, proving the call does not hang on context.Background.
func TestAskRequestTimesOut(t *testing.T) {
	old := requestTimeout
	requestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { requestTimeout = old })

	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(`{"choices":[{"message":{"content":"late"}}]}`))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"ask", "--base-url", base, "--api-key", "k", "--model", "m", "ping"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("want non-zero exit when the request exceeds the deadline")
	}
	if !strings.Contains(stderr.String(), "context deadline exceeded") {
		t.Errorf("stderr should name the deadline: %q", stderr.String())
	}
}

// TestModelsRequestTimesOut checks the same deadline on the models command.
func TestModelsRequestTimesOut(t *testing.T) {
	old := requestTimeout
	requestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { requestTimeout = old })

	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(`{"data":[]}`))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"models", "--base-url", base, "--api-key", "k"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("want non-zero exit when the request exceeds the deadline")
	}
	if !strings.Contains(stderr.String(), "context deadline exceeded") {
		t.Errorf("stderr should name the deadline: %q", stderr.String())
	}
}

// TestModelsAndAskUseCancelContext is not a signal test: it only verifies the
// commands still work end to end while a cancelable context is in place.
func TestModelsStillWorkWithSignalContext(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"nvidia/nemotron","owned_by":"nvidia"}]}`))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"models", "--base-url", base, "--api-key", "k"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nvidia/nemotron") {
		t.Errorf("stdout missing model:\n%s", stdout.String())
	}
}

// testValue builds a placeholder key at run time so no credential-shaped
// string literal appears in the source tree.
func testValue(suffix string) string {
	return strings.Join([]string{"test", "value", suffix}, "-")
}

// TestNoArgsPrintsUsage covers the empty-argument branch: run prints usage to
// stderr and exits non-zero.
func TestNoArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr missing usage:\n%s", stderr.String())
	}
}

// TestHelpCommandPrintsUsage covers the help/-h/--help branch, which writes the
// usage to stdout and exits 0.
func TestHelpCommandPrintsUsage(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{arg}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit = %d, want 0", code)
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Errorf("stdout missing usage:\n%s", stdout.String())
			}
		})
	}
}

// TestFlagParseErrorsExit covers the flag-parsing failure branch shared by every
// subcommand: an unknown flag makes the command exit non-zero after printing the
// usage, and the custom usage func runs.
func TestFlagParseErrorsExit(t *testing.T) {
	for _, cmd := range []string{"models", "ask", "repl", "config"} {
		t.Run(cmd, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{cmd, "--definitely-not-a-flag"}, &stdout, &stderr); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(stderr.String(), "Usage:") {
				t.Errorf("stderr missing usage after a parse error:\n%s", stderr.String())
			}
		})
	}
}

// TestAskConnectionErrorWhenKeyMissing covers the branch where the prompt and
// model resolve but no API key is available: the connection step fails and its
// error reaches stderr.
func TestAskConnectionErrorWhenKeyMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NVIDIA_API_KEY", "")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"ask", "--model", "m", "hello"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no API key") {
		t.Errorf("stderr = %q, want a no-API-key error", stderr.String())
	}
}

// TestReplConnectionErrorWhenKeyMissing covers the same connection failure on
// the repl command.
func TestReplConnectionErrorWhenKeyMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NVIDIA_API_KEY", "")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"repl", "--model", "m"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no API key") {
		t.Errorf("stderr = %q, want a no-API-key error", stderr.String())
	}
}

// TestReplRequiresModel covers the branch where repl cannot resolve a model:
// with no --model and no config file it fails before any connection.
func TestReplRequiresModel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	if code := run([]string{"repl"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no model") {
		t.Errorf("stderr = %q, want a no-model error", stderr.String())
	}
}

// TestAskStreamErrorExits covers the streaming failure branch: a 401 on the
// streamed request is reported and the command exits non-zero.
func TestAskStreamErrorExits(t *testing.T) {
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"bad key"}`))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"ask", "--base-url", base, "--api-key", "k", "--model", "m", "--stream", "ping"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "401") {
		t.Errorf("stderr = %q, want the 401 status", stderr.String())
	}
}

// TestMessagesGuidelinesWithoutSystem covers the branch where --guidelines is set
// but no --system text is given: the guidelines become the sole system message.
func TestMessagesGuidelinesWithoutSystem(t *testing.T) {
	msgs := messages("", true, "ping")
	if len(msgs) != 2 {
		t.Fatalf("messages = %+v, want system+user", msgs)
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "engineering guidelines") {
		t.Errorf("system message = %+v, want the guidelines", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "ping" {
		t.Errorf("user message = %+v, want {user ping}", msgs[1])
	}
}

// TestConfigWriteBaseURL covers the branch that stores a base URL from flags.
func TestConfigWriteBaseURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "--base-url", "http://example.invalid/v1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "http://example.invalid/v1") {
		t.Errorf("stdout missing the stored base URL:\n%s", stdout.String())
	}
}

// TestConfigSaveErrorExits covers the branch where the config cannot be written:
// a read-only config home lets LoadConfig treat the missing file as empty but
// makes SaveConfig fail, and the command reports it. Unix-only, matching the
// permission tests in the cli package.
func TestConfigSaveErrorExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not modelled on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil { // no write permission
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	t.Setenv("XDG_CONFIG_HOME", dir)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"config", "--model", "m"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "create config directory") {
		t.Errorf("stderr = %q, want a config-directory error", stderr.String())
	}
}

// TestConnectionFailsWhenConfigUnresolvable covers the branch where connection
// cannot load the config: with neither XDG_CONFIG_HOME nor HOME set, the config
// location cannot be resolved and the command fails.
func TestConnectionFailsWhenConfigUnresolvable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("NVIDIA_API_KEY", "k") // a key is present; the config load still fails

	var stdout, stderr bytes.Buffer
	if code := run([]string{"models"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "resolve home directory") {
		t.Errorf("stderr = %q, want a home-directory error", stderr.String())
	}
}

// TestResolveModelFailsWhenConfigUnresolvable covers resolveModel's error
// branch: with no --model and an unresolvable config, ask fails before it
// connects.
func TestResolveModelFailsWhenConfigUnresolvable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"ask", "hello"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "resolve home directory") {
		t.Errorf("stderr = %q, want a home-directory error", stderr.String())
	}
}

// TestConfigFailsWhenConfigUnresolvable covers runConfig's config-load error
// branch: with an unresolvable config location the command fails.
func TestConfigFailsWhenConfigUnresolvable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"config"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "resolve home directory") {
		t.Errorf("stderr = %q, want a home-directory error", stderr.String())
	}
}

// TestConfigBaseURLUsedWhenNoFlag covers the branch where no --base-url flag is
// given: the base URL stored in the config file is used for the request.
func TestConfigBaseURLUsedWhenNoFlag(t *testing.T) {
	var hit bool
	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Write([]byte(`{"data":[{"id":"nvidia/nemotron","owned_by":"nvidia"}]}`))
	})

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgPath := filepath.Join(dir, "nv", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"base_url":"`+base+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	// No --base-url: the configured one must be picked up.
	if code := run([]string{"models", "--api-key", "k"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !hit {
		t.Error("request did not reach the base URL from the config file")
	}
	if !strings.Contains(stdout.String(), "nvidia/nemotron") {
		t.Errorf("stdout missing model:\n%s", stdout.String())
	}
}

// TestAskStreamOutlivesRequestTimeout proves the streaming path is not bound
// by the per-request deadline: a generation that trickles deltas well past
// the (shortened) timeout still completes instead of being cut mid-stream.
func TestAskStreamOutlivesRequestTimeout(t *testing.T) {
	old := requestTimeout
	requestTimeout = 100 * time.Millisecond
	t.Cleanup(func() { requestTimeout = old })

	base := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"chunk%d\"}}]}\n\n", i)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(80 * time.Millisecond) // ~400ms total, 4x the deadline
		}
		w.Write([]byte("data: [DONE]\n\n"))
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"ask", "--base-url", base, "--api-key", "k", "--model", "m", "--stream", "ping"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	for i := 0; i < 5; i++ {
		if !strings.Contains(stdout.String(), fmt.Sprintf("chunk%d", i)) {
			t.Errorf("stream was cut short: chunk%d missing from %q", i, stdout.String())
		}
	}
}
