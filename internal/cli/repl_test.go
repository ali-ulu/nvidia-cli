package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	if code := session.Run(context.Background(), strings.NewReader(in)); code != 0 {
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
	if code := NewREPL(nim.New("k", srv.URL), &strings.Builder{}, "m").Run(context.Background(), strings.NewReader(in)); code != 0 {
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
		// 400 is a permanent client error, so exactly one request per turn:
		// the test stays decoupled from the client's retry budget.
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	in := "boom\nafter\n:quit\n"
	_ = NewREPL(nim.New("k", srv.URL), &strings.Builder{}, "m").Run(context.Background(), strings.NewReader(in))
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
	if code := session.Run(context.Background(), strings.NewReader(in)); code != 0 {
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

// historyCaptorServer records every request body and streams reply back.
func historyCaptorServer(t *testing.T, bodies *[]nim.ChatRequest, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body nim.ChatRequest
		_ = jsonDecode(r.Body, &body)
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + reply + "\"}}]}\n\ndata: [DONE]\n\n"))
	}))
}

func TestREPLTrimsOldestTurnsOverTokenBudget(t *testing.T) {
	var bodies []nim.ChatRequest
	srv := historyCaptorServer(t, &bodies, "ok")
	defer srv.Close()

	// Three turns of ~15k chars each (~3.75k tokens): the third request
	// carries ~11.2k tokens and must drop the oldest complete pair
	// (user + assistant), never an orphaned assistant reply.
	long := func(tag string) string { return tag + ":" + strings.Repeat("x", 15000) }
	in := long("turn-one") + "\n" + long("turn-two") + "\n" + long("turn-three") + "\n:quit\n"

	var out strings.Builder
	if code := NewREPL(nim.New("k", srv.URL), &out, "m").Run(context.Background(), strings.NewReader(in)); code != 0 {
		t.Fatal("Run exit != 0")
	}
	if len(bodies) != 3 {
		t.Fatalf("chat calls = %d, want 3", len(bodies))
	}
	last := bodies[2]
	for _, msg := range last.Messages {
		if strings.Contains(msg.Content, "turn-one") {
			t.Errorf("oldest turn survived trimming: %+v", msg)
		}
	}
	// Role order must stay well formed: user,assistant,user — an assistant
	// message must never lead the history after its user turn was dropped.
	var roles []string
	for _, msg := range last.Messages {
		roles = append(roles, msg.Role)
	}
	if strings.Join(roles, ",") != "user,assistant,user" {
		t.Errorf("turn-3 roles = %v, want [user assistant user]: %+v", roles, last.Messages)
	}
	if last.Messages[0].Content != long("turn-two") {
		t.Errorf("oldest surviving turn = %q..., want turn-two", last.Messages[0].Content[:20])
	}
	if last.Messages[len(last.Messages)-1].Content != long("turn-three") {
		t.Errorf("newest user turn was dropped: %+v", last.Messages)
	}
	if !strings.Contains(out.String(), "trimmed") {
		t.Errorf("missing trim notice in output:\n%s", out.String())
	}
}

// TestTrimHistoryDropsWholePairs pins the pairing rule directly: when the
// budget forces a drop, exactly a user+assistant pair goes, and the remaining
// history never starts with an assistant reply.
func TestTrimHistoryDropsWholePairs(t *testing.T) {
	long := strings.Repeat("x", 16000) // ~4000 tokens
	msgs := []nim.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: long},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: long},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "latest"},
	}
	// 1 + 4000 + 1 + 4000 + 1 + 1 = 8004 > 8000: the first pair must go.
	trimmed, dropped := trimHistory(msgs, maxHistoryTokens)
	if dropped != 2 {
		t.Errorf("dropped = %d messages, want the whole first pair (2)", dropped)
	}
	var roles []string
	for _, m := range trimmed {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "system,user,assistant,user" {
		t.Errorf("roles = %v, want [system user assistant user]: %+v", roles, trimmed)
	}
	if trimmed[1].Content != long || trimmed[3].Content != "latest" {
		t.Errorf("wrong messages survived: %+v", trimmed)
	}
}

// TestTrimHistoryKeepsFittingHistoryUntouched guards the no-op path.
func TestTrimHistoryKeepsFittingHistoryUntouched(t *testing.T) {
	msgs := []nim.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	trimmed, dropped := trimHistory(msgs, maxHistoryTokens)
	if dropped != 0 || len(trimmed) != len(msgs) {
		t.Errorf("fitting history was altered: dropped=%d msgs=%+v", dropped, trimmed)
	}
}

// TestREPLCanceledContextDropsTurnAndContinues drives the interrupt branch by
// handing Run an already-canceled context (what SIGINT produces per turn):
// the turn is dropped before any chat call, "^C" is printed, and :quit still
// exits cleanly.
func TestREPLCanceledContextDropsTurnAndContinues(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out strings.Builder
	in := "one\n:quit\n"
	if code := NewREPL(nim.New("k", srv.URL), &out, "m").Run(ctx, strings.NewReader(in)); code != 0 {
		t.Fatalf("Run exit = %d, want 0 (output: %s)", code, out.String())
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("interrupted turn reached the server (%d calls)", calls)
	}
	if !strings.Contains(out.String(), "^C") {
		t.Errorf("missing ^C notice in output:\n%s", out.String())
	}
}

func TestREPLSystemMessageSurvivesTrimming(t *testing.T) {
	var bodies []nim.ChatRequest
	srv := historyCaptorServer(t, &bodies, "ok")
	defer srv.Close()

	long := strings.Repeat("y", 15000)
	in := long + "\n" + long + "\n" + long + "\n:quit\n"
	session := NewREPL(nim.New("k", srv.URL), &strings.Builder{}, "m")
	session.System = "keep me"

	if code := session.Run(context.Background(), strings.NewReader(in)); code != 0 {
		t.Fatal("Run exit != 0")
	}
	if len(bodies) != 3 {
		t.Fatalf("chat calls = %d, want 3", len(bodies))
	}
	last := bodies[2]
	if len(last.Messages) == 0 || last.Messages[0].Role != "system" || last.Messages[0].Content != "keep me" {
		t.Errorf("system message not first after trimming: %+v", last.Messages)
	}
	// The newest user turn is never dropped even when it alone is huge.
	if last.Messages[len(last.Messages)-1].Content != long {
		t.Errorf("newest user turn was truncated or dropped")
	}
}

func TestREPLAcceptsLongInputLine(t *testing.T) {
	var bodies []nim.ChatRequest
	srv := historyCaptorServer(t, &bodies, "ok")
	defer srv.Close()

	// 100 KiB on one line: above the 64 KiB bufio.Scanner default limit.
	line := strings.Repeat("z", 100<<10)
	in := line + "\n:quit\n"
	if code := NewREPL(nim.New("k", srv.URL), &strings.Builder{}, "m").Run(context.Background(), strings.NewReader(in)); code != 0 {
		t.Fatal("Run exit != 0")
	}
	if len(bodies) != 1 {
		t.Fatalf("chat calls = %d, want 1", len(bodies))
	}
	if got := bodies[0].Messages[0].Content; got != line {
		t.Errorf("long line not delivered intact: got %d chars, want %d", len(got), len(line))
	}
}
