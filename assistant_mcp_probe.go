package main

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// assistant_mcp_probe.go verifies the remote Streamable HTTP endpoint without
// doing network work in the immediate-mode settings view.
type mcpBearerTransport struct {
	token string
	next  http.RoundTripper
}

func (t mcpBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.next.RoundTrip(clone)
}

func probeRemoteMCP(ctx context.Context, endpoint, token string) (int, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "sunvale-settings", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint,
		HTTPClient: &http.Client{Transport: mcpBearerTransport{
			token: token,
			next:  http.DefaultTransport,
		}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return 0, err
	}
	defer session.Close()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return 0, err
	}
	return len(result.Tools), nil
}
