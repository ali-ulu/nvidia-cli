package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(stdout.String(), testValue("stored")) || !strings.Contains(stdout.String(), "m1") {
		t.Errorf("write output missing fields:\n%s", stdout.String())
	}

	stdout.Reset()
	if code := run([]string{"config"}, &stdout, &stderr); code != 0 {
		t.Fatalf("read exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "config file:") || !strings.Contains(stdout.String(), "m1") {
		t.Errorf("read output missing stored config:\n%s", stdout.String())
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

// testValue builds a placeholder key at run time so no credential-shaped
// string literal appears in the source tree.
func testValue(suffix string) string {
	return strings.Join([]string{"test", "value", suffix}, "-")
}
