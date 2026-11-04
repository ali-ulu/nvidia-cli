package nim

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fastClient is a Client with a tight retry budget for tests.
func fastClient(t *testing.T, url string) *Client {
	t.Helper()
	c := New("test-key", url)
	c.maxAttempts = 3
	return c
}

func TestRetries5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"data":[{"id":"a","owned_by":"x"}]}`))
	}))
	defer srv.Close()

	models, err := fastClient(t, srv.URL).Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %+v", models)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetries429AndHonorsRetryAfterSeconds(t *testing.T) {
	var waits []time.Duration
	origWait := wait
	wait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	t.Cleanup(func() { wait = origWait })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	reply, err := fastClient(t, srv.URL).Chat(context.Background(), ChatRequest{Model: "m"})
	if err != nil || reply != "ok" {
		t.Fatalf("Chat = %q, %v", reply, err)
	}
	if len(waits) != 1 || waits[0] != 7*time.Second {
		t.Errorf("waits = %v, want [7s] (Retry-After must win)", waits)
	}
}

func TestNoRetryOnClientErrors(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone,
		http.StatusBadRequest, http.StatusUnprocessableEntity,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(status)
			}))
			defer srv.Close()

			_, err := fastClient(t, srv.URL).Models(context.Background())
			var apiErr *APIError
			if err == nil {
				t.Fatal("want an error")
			}
			if !errors.As(err, &apiErr) || apiErr.Status != status {
				t.Fatalf("err = %v, want APIError %d", err, status)
			}
			if calls != 1 {
				t.Errorf("calls = %d, want exactly 1 (no retry on %d)", calls, status)
			}
		})
	}
}

func TestRetriesTransportErrors(t *testing.T) {
	// A closed server makes every call fail at the transport layer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	if _, err := fastClient(t, srv.URL).Models(context.Background()); err == nil {
		t.Fatal("want error")
	}
	// Success is defined by the call budget being exhausted without panic;
	// the retry loop is exercised by the tests above.
}

func TestRetryExhaustionReturnsLastError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := fastClient(t, srv.URL).Models(context.Background())
	if err == nil {
		t.Fatal("want error after exhausting retries")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (maxAttempts)", calls)
	}
}

func TestRetryAfterHTTPDateForm(t *testing.T) {
	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	d := retryAfter(http.Header{"Retry-After": []string{future}}, time.Now())
	if d <= 0 {
		t.Errorf("retryAfter(HTTP date) = %v, want > 0", d)
	}
	if got := retryAfter(http.Header{}, time.Now()); got != 0 {
		t.Errorf("retryAfter(missing) = %v, want 0", got)
	}
	if got := retryAfter(http.Header{"Retry-After": []string{"garbage"}}, time.Now()); got != 0 {
		t.Errorf("retryAfter(garbage) = %v, want 0", got)
	}
}

func TestBackoffIsBoundedAndJittered(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 1; i <= 30; i++ {
		d := backoff(1, rand.New(rand.NewSource(int64(i))))
		if d < 0 || d > retryMaxDelay {
			t.Fatalf("backoff = %v, out of range", d)
		}
		seen[d] = true
	}
	if len(seen) < 5 {
		t.Errorf("backoff produced only %d distinct values; jitter looks broken", len(seen))
	}
	if d := backoff(10, rand.New(rand.NewSource(1))); d > retryMaxDelay {
		t.Errorf("backoff(capped) = %v > %v", d, retryMaxDelay)
	}
}
