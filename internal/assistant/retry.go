package assistant

import (
	"context"
	"errors"
	"net"
	"strings"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
)

// TransientModelError reports whether a ChatModel failure is worth a short
// automatic retry. Rate limits are excluded: the pane already switches routes
// for 429, and retrying them here would multiply requests into a storm.
// User cancellations are never transient. A dial/read timeout (net.Error) is.
func TransientModelError(err error) bool {
	if err == nil {
		return false
	}
	// Context sentinels first: deadlineExceededError also implements net.Error,
	// and must not be treated as a dial timeout worth retrying.
	if errors.Is(err, ErrStopped) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if _, ok := AsRateLimit(err); ok {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var api *openai.APIError
	if errors.As(err, &api) {
		switch {
		case api.HTTPStatusCode == 408, api.HTTPStatusCode == 409:
			return true
		case api.HTTPStatusCode >= 500:
			return true
		case api.HTTPStatusCode >= 400:
			return false
		}
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "429") || strings.Contains(text, "rate_limit") || strings.Contains(text, "频率限制") {
		return false
	}
	for _, needle := range []string{
		"timeout", "timed out", "temporary failure", "connection reset",
		"connection refused", "broken pipe", "tls handshake",
		"unexpected eof", "i/o timeout", "server misbehaving",
		"status code: 500", "status code: 502", "status code: 503", "status code: 504",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
