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
	"net/http"
	"strings"
)

// DefaultBaseURL is the hosted NVIDIA NIM endpoint.
const DefaultBaseURL = "https://integrate.api.nvidia.com/v1"

// maxBodyBytes caps how much of a response is buffered.
const maxBodyBytes = 32 << 20

// Client talks to a NIM-compatible API.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
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
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(buf)
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
		return &APIError{Status: resp.StatusCode, Body: string(payload)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}
