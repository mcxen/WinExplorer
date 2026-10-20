package remote

import (
	"encoding/json"
	"io"
	"net/http"
)

// ReadJSON decodes a small JSON request body.
func ReadJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
