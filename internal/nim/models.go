package nim

import (
	"context"
	"net/http"
)

// Models lists the models the API currently serves.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var out modelsResponse
	if err := c.do(ctx, http.MethodGet, "/models", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
