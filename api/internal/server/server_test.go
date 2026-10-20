package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/stretchr/testify/require"
)

func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	return New(cfg, slogDiscard(), "test")
}

func TestHealthzReturns200(t *testing.T) {
	s := newTestServer(t)

	resp, err := s.App().Test(httpGet("/healthz"))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"status":"ok"`)
}

func TestReadyzReadyWithNoChecks(t *testing.T) {
	s := newTestServer(t)

	resp, err := s.App().Test(httpGet("/readyz"))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestReadyzFailsWhenCheckFails(t *testing.T) {
	s := newTestServer(t)
	s.RegisterReadinessCheck("postgres", func() error {
		return errors.New("connection refused")
	})

	resp, err := s.App().Test(httpGet("/readyz"))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

// The failure body must expose only the dependency's up/down state, never the
// raw probe error (which can carry internal hostnames/URLs) — see 06-security.md.
func TestReadyzSanitizesCheckError(t *testing.T) {
	s := newTestServer(t)
	s.RegisterReadinessCheck("postgres", func() error {
		return errors.New("dial tcp 10.0.0.5:5432: connection refused")
	})

	resp, err := s.App().Test(httpGet("/readyz"))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"postgres":"unavailable"`)
	require.NotContains(t, string(body), "10.0.0.5")
	require.NotContains(t, string(body), "connection refused")
}

// A same-origin browser request carries the (chunked) session cookie, so the
// request head can be several KiB. The server must accept it rather than 431 —
// fasthttp's default 4 KiB read buffer would reject it (guards the raised
// ReadBufferSize in New).
func TestLargeCookieHeaderIsAccepted(t *testing.T) {
	s := newTestServer(t)

	req := httpGet("/healthz")
	req.Header.Set("Cookie", "lkb_session.0="+strings.Repeat("a", 8000))

	resp, err := s.App().Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func httpGet(target string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	return req
}
