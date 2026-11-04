package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ali-ulu/nvidia-cli/internal/nim"
)

// jsonDecode decodes r's JSON body into v.
func jsonDecode(r interface{ Read([]byte) (int, error) }, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// sseChatServer answers every chat call with one streamed reply.
func sseChatServer(t *testing.T, reply string, seen *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = append(*seen, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + reply + "\"}}]}\n\ndata: [DONE]\n\n"))
	}))
}

func TestREPLMultiTurnAndCommands(t *testing.T) {
	var paths []string
	srv := sseChatServer(t, "hi", &paths)
	defer srv.Close()

	in := strings.Join([]string{
		":model other-model",
		"first turn",
		":reset",
		":unknown-cmd",
		"second turn",
		":quit",
		"never reached",
	}, "\n") + "\n"

	var out strings.Builder
	session := NewREPL(nim.New("k", srv.URL), &out, "default-model")
	if code := session.Run(strings.NewReader(in)); code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}
	got := out.String()
	if !strings.Contains(got, "model set to other-model") {
		t.Errorf("output missing :model confirmation:\n%s", got)
	}
	if !strings.Contains(got, "history cleared") {
		t.Errorf("output missing :reset confirmation:\n%s", got)
	}
	if !strings.Contains(got, "unknown command") {
		t.Errorf("output missing unknown-command notice:\n%s", got)
	}
	if strings.Count(got, "hi\n") < 2 { // two streamed replies
		t.Errorf("output missing streamed replies:\n%s", got)
	}
	// After :quit nothing further runs: exactly two chat turns happened.
	if len(paths) != 2 {
		t.Errorf("chat calls = %d, want 2", len(paths))
	}
}

func TestREPLKeepsHistoryAcrossTurns(t *testing.T) {
	var bodies []nim.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body nim.ChatRequest
		_ = jsonDecode(r.Body, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	in := "one\ntwo\n:quit\n"
	if code := NewREPL(nim.New("k", srv.URL), &strings.Builder{}, "m").Run(strings.NewReader(in)); code != 0 {
		t.Fatal("Run exit != 0")
	}
	if len(bodies) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(bodies))
	}
	// Turn two must carry turn one's user message and the assistant reply.
	want := []string{"user:one", "assistant:ok", "user:two"}
	var got []string
	for _, msg := range bodies[1].Messages {
		got = append(got, msg.Role+":"+msg.Content)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("turn-2 messages = %v, want %v", got, want)
	}
}

func TestREPLFailedTurnNotKept(t *testing.T) {
	var bodies []nim.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body nim.ChatRequest
		_ = jsonDecode(r.Body, &body)
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	in := "boom\nafter\n:quit\n"
	_ = NewREPL(nim.New("k", srv.URL), &strings.Builder{}, "m").Run(strings.NewReader(in))
	if len(bodies) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(bodies))
	}
	// Second turn carries only the new user message: the failed one was dropped.
	if len(bodies[1].Messages) != 1 || bodies[1].Messages[0].Content != "after" {
		t.Errorf("turn-2 messages = %+v, want only {user after}", bodies[1].Messages)
	}
}

func TestREPLSystemPromptAndGuidelines(t *testing.T) {
	var bodies []nim.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body nim.ChatRequest
		_ = jsonDecode(r.Body, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	in := "hello\n:quit\n"
	session := NewREPL(nim.New("k", srv.URL), &strings.Builder{}, "m")
	session.System = "be brief"
	session.UseGuidelines = true
	if code := session.Run(strings.NewReader(in)); code != 0 {
		t.Fatal("Run exit != 0")
	}
	if len(bodies) == 0 || len(bodies[0].Messages) == 0 {
		t.Fatal("no messages captured")
	}
	first := bodies[0].Messages[0]
	if first.Role != "system" || !strings.Contains(first.Content, "be brief") {
		t.Errorf("system message = %+v", first)
	}
}
