package assistant

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestTransientModelError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"stopped", ErrStopped, false},
		{"canceled", context.Canceled, false},
		{"wrapped canceled", errors.Join(errors.New("wrap"), context.Canceled), false},
		{"429 api", &openai.APIError{HTTPStatusCode: 429, Type: "rate_limit_error", Message: "slow down"}, false},
		{"429 text", errors.New("error, status code: 429, message: 频率限制"), false},
		{"401", &openai.APIError{HTTPStatusCode: 401, Message: "bad key"}, false},
		{"400", &openai.APIError{HTTPStatusCode: 400, Message: "bad request"}, false},
		{"500", &openai.APIError{HTTPStatusCode: 500, Message: "boom"}, true},
		{"503", &openai.APIError{HTTPStatusCode: 503, Message: "busy"}, true},
		{"net timeout", timeoutErr{}, true},
		{"reset text", errors.New("read: connection reset by peer"), true},
		{"deadline bare", context.DeadlineExceeded, false},
		{"other", errors.New("model refused"), false},
	}
	for _, tc := range cases {
		if got := TransientModelError(tc.err); got != tc.want {
			t.Fatalf("%s: TransientModelError(%v)=%v want %v", tc.name, tc.err, got, tc.want)
		}
	}
	// A dial that surfaces as DeadlineExceeded via net.Error still retries.
	err := &net.OpError{Op: "dial", Err: timeoutErr{}}
	if !TransientModelError(err) {
		t.Fatalf("dial timeout should retry: %v", err)
	}
	_ = time.Second
}
