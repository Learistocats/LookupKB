package health

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPCheckReadyOn2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := HTTPCheck("dep", srv.URL, time.Second)()
	require.NoError(t, err)
}

func TestHTTPCheckFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := HTTPCheck("dep", srv.URL, time.Second)()
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

func TestHTTPCheckFailsOnTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release // hang until the test releases it, forcing the client timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(release)

	err := HTTPCheck("dep", srv.URL, 50*time.Millisecond)()
	require.Error(t, err)
}

func TestHTTPCheckFailsWhenUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	err := HTTPCheck("dep", url, 200*time.Millisecond)()
	require.Error(t, err)
}

func TestCachedCollapsesCallsWithinTTL(t *testing.T) {
	var calls int32
	sentinel := errors.New("boom")
	check := Cached(time.Hour, func() error {
		atomic.AddInt32(&calls, 1)
		return sentinel
	})

	for i := 0; i < 5; i++ {
		require.ErrorIs(t, check(), sentinel)
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "probe should run once within the TTL")
}

func TestCachedRefreshesAfterTTL(t *testing.T) {
	var calls int32
	check := Cached(10*time.Millisecond, func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	require.NoError(t, check())
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, check())
	require.Equal(t, int32(2), atomic.LoadInt32(&calls), "probe should re-run after the TTL elapses")
}

func TestCachedZeroTTLAlwaysCalls(t *testing.T) {
	var calls int32
	check := Cached(0, func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	for i := 0; i < 3; i++ {
		require.NoError(t, check())
	}
	require.Equal(t, int32(3), atomic.LoadInt32(&calls))
}
