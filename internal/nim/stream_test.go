package nim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// errStop is the error the onDelta callback returns to abort the stream.
var errStop = errors.New("stop")

// partialSSEServer flushes a fixed byte sequence in tiny, mid-line splits.
func partialSSEServer(t *testing.T, writes []string, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range writes {
			_, _ = w.Write([]byte(part))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(delay)
		}
	}))
}

func TestChatStreamAssemblesPartialChunks(t *testing.T) {
	// Each SSE event is split mid-line across separate writes; "delta" is
	// cut in half, "[DONE]" too.
	srv := partialSSEServer(t, []string{
		"data: {\"choices\":[{\"del",
		"ta\":{\"content\":\"he\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"l",
		"lo wo\"}}]}\n",
		"\n",
		"data: [DO",
		"NE]\n\n",
	}, 5*time.Millisecond)
	defer srv.Close()

	var deltas []string
	reply, err := New("test-key", srv.URL).ChatStream(context.Background(), ChatRequest{Model: "m"}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if reply != "hello wo" {
		t.Errorf("reply = %q, want %q", reply, "hello wo")
	}
	if strings.Join(deltas, "") != "hello wo" {
		t.Errorf("deltas = %q, want concatenated %q", deltas, "hello wo")
	}
}

func TestChatStreamSendsStreamTrueAndAcceptHeader(t *testing.T) {
	var gotAccept, gotPath string
	var body ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept, gotPath = r.Header.Get("Accept"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	if _, err := New("test-key", srv.URL).ChatStream(context.Background(), ChatRequest{Model: "m"}, nil); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if !body.Stream {
		t.Error("request body must carry stream:true")
	}
}

func TestChatStreamNon2xxReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	_, err := New("test-key", srv.URL).ChatStream(context.Background(), ChatRequest{Model: "m"}, nil)
	var apiErr *APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want APIError 401", err)
	}
}

func TestChatStreamEmptyStreamFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(": keepalive\n\n"))
	}))
	defer srv.Close()

	if _, err := New("test-key", srv.URL).ChatStream(context.Background(), ChatRequest{Model: "m"}, nil); err == nil {
		t.Fatal("want error for empty stream")
	}
}

func TestChatStreamOnDeltaErrorStops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n"))
	}))
	defer srv.Close()

	_, err := New("test-key", srv.URL).ChatStream(context.Background(), ChatRequest{Model: "m"}, func(string) error {
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("err = %v, want the onDelta error to propagate", err)
	}
}

// noWaitFastClient stubs out the retry sleep and keeps a small attempt budget.
func noWaitFastClient(t *testing.T, url string) *Client {
	t.Helper()
	origWait := wait
	wait = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { wait = origWait })
	c := New("test-key", url)
	c.maxAttempts = 3
	return c
}

func TestChatStreamRetries5xxBeforeFirstToken(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"second\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	var deltas []string
	reply, err := noWaitFastClient(t, srv.URL).ChatStream(context.Background(), ChatRequest{Model: "m"}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if reply != "second" {
		t.Errorf("reply = %q, want the second attempt's reply", reply)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if strings.Join(deltas, "") != "second" {
		t.Errorf("deltas = %v, want output only from the successful attempt", deltas)
	}
}

func TestChatStreamRetriesTransportErrorBeforeHeaders(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			panic(http.ErrAbortHandler) // kill the connection before any header
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	reply, err := noWaitFastClient(t, srv.URL).ChatStream(context.Background(), ChatRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if reply != "ok" {
		t.Errorf("reply = %q, want %q", reply, "ok")
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestChatStreamDoesNotRetryOnceStreamStarted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) > 1 {
			t.Errorf("a stream error after 2xx headers must not be retried")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		panic(http.ErrAbortHandler) // abort mid-stream, after the first token
	}))
	defer srv.Close()

	var got []string
	_, err := noWaitFastClient(t, srv.URL).ChatStream(context.Background(), ChatRequest{Model: "m"}, func(d string) error {
		got = append(got, d)
		return nil
	})
	if err == nil {
		t.Fatal("want error from the aborted stream")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("calls = %d, want exactly 1 (no retry after stream start)", calls)
	}
	if strings.Join(got, "") != "first" {
		t.Errorf("deltas = %v, want only the first attempt's token", got)
	}
}
