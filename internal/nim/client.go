// Package nim is a minimal client for the NVIDIA NIM inference API.
//
// NIM speaks the OpenAI wire protocol, so only the two endpoints the CLI needs
// are implemented: model listing and chat completions.
package nim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the hosted NVIDIA NIM endpoint.
const DefaultBaseURL = "https://integrate.api.nvidia.com/v1"

// maxBodyBytes caps how much of a response is buffered.
const maxBodyBytes = 32 << 20

// Client talks to a NIM-compatible API.
type Client struct {
	baseURL     string
	apiKey      string
	http        *http.Client
	maxAttempts int        // retry budget; 0 means the default of 4
	rnd         *rand.Rand // jitter source for backoff
}

// New returns a Client. An empty baseURL falls back to DefaultBaseURL.
func New(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{},
		rnd:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	marshal := func() (io.Reader, error) {
		if body == nil {
			return nil, nil
		}
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		return bytes.NewReader(buf), nil
	}

	var lastHeaders http.Header
	err := c.retry(ctx, func() error {
		reader, err := marshal()
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("call %s: %w", path, err)
		}
		// The body is read in full below, so a close error carries no information.
		defer func() { _ = resp.Body.Close() }()

		payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			return fmt.Errorf("read %s response: %w", path, err)
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			lastHeaders = resp.Header
			return &APIError{Status: resp.StatusCode, Body: string(payload)}
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
		return nil
	}, func() http.Header { return lastHeaders })
	return err
}
