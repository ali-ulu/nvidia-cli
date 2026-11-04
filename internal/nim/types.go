package nim

import (
	"fmt"
	"strings"
)

// Message is a single turn in a conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the body of POST /chat/completions.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// Model is one entry from GET /models.
type Model struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

type modelsResponse struct {
	Data []Model `json:"data"`
}

// APIError is a non-2xx response from the API.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	detail := strings.TrimSpace(e.Body)
	if len(detail) > 300 {
		detail = detail[:300] + "..."
	}
	if detail == "" {
		return fmt.Sprintf("api returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("api returned HTTP %d: %s", e.Status, detail)
}
