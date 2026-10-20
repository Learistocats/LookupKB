package mcpserver

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// bearerTransport adds the credential to every request, as an MCP client
// configured with an Authorization header does.
type bearerTransport struct{ authz string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", b.authz)
	return http.DefaultTransport.RoundTrip(r)
}

// TestOfficialClientInterop drives the endpoint over a real TCP listener with the
// official SDK's Streamable HTTP client (default options, including its attempt to
// open the optional standalone SSE stream, which this server declines with 405).
func TestOfficialClientInterop(t *testing.T) {
	h := newHarness(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = h.app.Listener(ln) }()
	t.Cleanup(func() { _ = h.app.Shutdown() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "interop", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   "http://" + ln.Addr().String() + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{authz: h.token}},
		MaxRetries: -1,
	}, nil)
	require.NoError(t, err)
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 4)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: ToolGetDocument, Arguments: map[string]any{"slug": "kube-1"}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Content[0].(*mcp.TextContent).Text, "# Kube")

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: ToolGetDocument, Arguments: map[string]any{"slug": "nope"}})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Equal(t, "document not found", res.Content[0].(*mcp.TextContent).Text)
}
