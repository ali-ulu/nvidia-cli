package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
		{"no model", []string{"ask", "hello"}, "needs --model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(append([]string{"--api-key"}, "k"), &stdout, &stderr)
			_ = code
			code = run(tc.args, &stdout, &stderr)
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
