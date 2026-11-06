package nim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a Client at a real local HTTP server.
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New("test-key", srv.URL), srv
}

func TestModelsReturnsParsedList(t *testing.T) {
	var gotAuth, gotPath string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"a/b","owned_by":"a"},{"id":"c/d","owned_by":"c"}]}`))
	})

	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 || models[0].ID != "a/b" || models[1].OwnedBy != "c" {
		t.Fatalf("unexpected models: %+v", models)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if gotPath != "/models" {
		t.Errorf("path = %q, want /models", gotPath)
	}
}

func TestChatSendsMessagesAndReturnsContent(t *testing.T) {
	var body ChatRequest
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	})

	reply, err := c.Chat(context.Background(), ChatRequest{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply != "hello" {
		t.Errorf("reply = %q, want hello", reply)
	}
	if body.Model != "m" || len(body.Messages) != 1 || body.Messages[0].Content != "hi" {
		t.Errorf("unexpected request body: %+v", body)
	}
}

func TestChatWithoutChoicesFails(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[]}`))
	})
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "m"}); err == nil {
		t.Fatal("want error for empty choices, got nil")
	}
}

// TestChatPropagatesRequestError covers the branch where the underlying request
// fails: Chat must surface that error instead of returning a partial reply.
func TestChatPropagatesRequestError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"bad key"}`))
	})
	reply, err := c.Chat(context.Background(), ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("want error for a 401 response")
	}
	if reply != "" {
		t.Errorf("reply = %q, want empty on error", reply)
	}
}

func TestNon2xxReturnsAPIError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error":"invalid key"}`},
		{"not found", http.StatusNotFound, `{"error":"model not found"}`},
		{"gone", http.StatusGone, `{"error":"model reached end of life"}`},
		{"server error", http.StatusInternalServerError, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})

			_, err := c.Models(context.Background())
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("want *APIError, got %T: %v", err, err)
			}
			if apiErr.Status != tc.status {
				t.Errorf("Status = %d, want %d", apiErr.Status, tc.status)
			}
			if tc.body != "" && !strings.Contains(apiErr.Error(), "invalid key") && !strings.Contains(apiErr.Error(), tc.body) {
				t.Errorf("Error() = %q, should surface the body", apiErr.Error())
			}
		})
	}
}

func TestMalformedJSONFails(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": not json`))
	})
	if _, err := c.Models(context.Background()); err == nil {
		t.Fatal("want decode error, got nil")
	}
}

func TestNewDefaultsBaseURL(t *testing.T) {
	if got := New("k", "").baseURL; got != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", got, DefaultBaseURL)
	}
	if got := New("k", "http://x/v1/").baseURL; got != "http://x/v1" {
		t.Errorf("trailing slash not trimmed: %q", got)
	}
}

// TestDoEncodeRequestError covers the marshal failure path: a body that cannot
// be encoded as JSON must fail before any request is built. The retry sleep is
// stubbed out because an encode failure is retryable.
func TestDoEncodeRequestError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	c = noWaitFastClientOn(t, c)
	err := c.do(context.Background(), http.MethodPost, "/chat/completions", make(chan int), nil)
	if err == nil || !strings.Contains(err.Error(), "encode request") {
		t.Fatalf("err = %v, want an encode-request error", err)
	}
}

// TestDoBuildRequestError covers the http.NewRequestWithContext failure path: an
// invalid method is rejected before the call is sent. The retry sleep is stubbed
// out because a build failure is retryable.
func TestDoBuildRequestError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	c = noWaitFastClientOn(t, c)
	err := c.do(context.Background(), "BAD METHOD", "/models", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("err = %v, want a build-request error", err)
	}
}

// TestDoNilOutSkipsDecode covers the out == nil branch: a successful response
// with no destination must not attempt to decode the payload.
func TestDoNilOutSkipsDecode(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"a","owned_by":"x"}]}`))
	})
	if err := c.do(context.Background(), http.MethodGet, "/models", nil, nil); err != nil {
		t.Fatalf("do(out=nil) = %v, want nil", err)
	}
}

// TestDoReadResponseError covers the io.ReadAll failure path: the server
// advertises a body longer than it sends, so the read fails with unexpected EOF.
// The retry sleep is stubbed out because a read failure is retryable.
func TestDoReadResponseError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte(`{"data":`)) // 8 bytes, far short of the advertised 100
	})
	c = noWaitFastClientOn(t, c)
	err := c.do(context.Background(), http.MethodGet, "/models", nil, new(modelsResponse))
	if err == nil || !strings.Contains(err.Error(), "read /models response") {
		t.Fatalf("err = %v, want a read-response error", err)
	}
}
