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
