package nim

import (
	"context"
	"errors"
	"net/http"
)

// Chat sends req and returns the first choice's text.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (string, error) {
	var out chatResponse
	if err := c.do(ctx, http.MethodPost, "/chat/completions", req, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("api returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}
