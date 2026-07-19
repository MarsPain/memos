package ai_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/ai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestProviderTransportRejectsUnsafeDestinationsAndRedirects(t *testing.T) {
	lookup := func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "public.example" {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://private.example/secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})
	client := ai.NewHTTPClient(ai.TransportConfig{LookupIP: lookup, Base: base})

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://public.example/v1", nil)
	require.NoError(t, err)
	_, err = client.Do(request)
	require.ErrorContains(t, err, "private-network")

	for _, endpoint := range []string{
		"ftp://public.example/v1",
		"https://user:pass@public.example/v1",
		"https://public.example/v1#fragment",
		"https://public.example/v1?api_key=secret",
	} {
		_, err := ai.ValidateEndpoint(endpoint, false)
		require.Error(t, err, endpoint)
	}
}

func TestProviderTransportBoundsRetries(t *testing.T) {
	attempts := 0
	client := ai.NewHTTPClient(ai.TransportConfig{
		Limits: ai.TransportLimits{MaxRetries: 1},
		LookupIP: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		},
		Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			status := http.StatusServiceUnavailable
			if attempts == 2 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: request}, nil
		}),
	})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://public.example/v1", bytes.NewReader([]byte("body")))
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, 2, attempts)
}

func TestProviderTransportBoundsRequestResponseAndTotalTime(t *testing.T) {
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	}
	t.Run("request size", func(t *testing.T) {
		client := ai.NewHTTPClient(ai.TransportConfig{
			Limits: ai.TransportLimits{MaxRequestBytes: 4}, LookupIP: lookup,
			Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: request}, nil
			}),
		})
		request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://public.example/v1", strings.NewReader("12345"))
		require.NoError(t, err)
		_, err = client.Do(request)
		require.ErrorContains(t, err, "request exceeds")
	})

	t.Run("response size", func(t *testing.T) {
		client := ai.NewHTTPClient(ai.TransportConfig{
			Limits: ai.TransportLimits{MaxResponseBytes: 4}, LookupIP: lookup,
			Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("12345")), Request: request}, nil
			}),
		})
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://public.example/v1", nil)
		require.NoError(t, err)
		response, err := client.Do(request)
		require.NoError(t, err)
		_, err = io.ReadAll(response.Body)
		require.ErrorContains(t, err, "response exceeds")
	})

	t.Run("total time", func(t *testing.T) {
		client := ai.NewHTTPClient(ai.TransportConfig{
			Limits: ai.TransportLimits{TotalTimeout: 10 * time.Millisecond}, LookupIP: lookup,
			Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				<-request.Context().Done()
				return nil, request.Context().Err()
			}),
		})
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://public.example/v1", nil)
		require.NoError(t, err)
		_, err = client.Do(request)
		require.Error(t, err)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
