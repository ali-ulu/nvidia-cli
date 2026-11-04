package nim

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// streamChunk is one data payload of an SSE chat-completions stream.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// ChatStream sends req with stream=true and calls onDelta for every non-empty
// content delta as it arrives. It returns the full concatenated reply.
//
// The SSE stream is parsed incrementally: chunks split across TCP segments or
// flushed mid-line are handled by the buffered reader, not assumed whole.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, onDelta func(string) error) (string, error) {
	req.Stream = true
	return c.stream(ctx, "/chat/completions", req, onDelta)
}

func (c *Client) stream(ctx context.Context, path string, body any, onDelta func(string) error) (string, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			return "", fmt.Errorf("read %s response: %w", path, err)
		}
		return "", &APIError{Status: resp.StatusCode, Body: string(payload)}
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxBodyBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // comments, event:/id: fields, blank separators
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return full.String(), fmt.Errorf("decode %s stream chunk: %w", path, err)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			full.WriteString(choice.Delta.Content)
			if onDelta != nil {
				if err := onDelta(choice.Delta.Content); err != nil {
					return full.String(), err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return full.String(), fmt.Errorf("read %s stream: %w", path, err)
	}
	if full.Len() == 0 {
		return "", errors.New("api returned an empty stream")
	}
	return full.String(), nil
}
