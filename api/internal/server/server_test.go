package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
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

func httpGet(target string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	return req
}
