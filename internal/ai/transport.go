package ai

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// TransportLimits bounds all provider traffic.
type TransportLimits struct {
	DNSAndDialTimeout     time.Duration
	ResponseHeaderTimeout time.Duration
	TotalTimeout          time.Duration
	MaxRequestBytes       int64
	MaxResponseBytes      int64
	MaxRedirects          int
	MaxRetries            int
}

// DefaultTransportLimits returns conservative provider-call limits.
func DefaultTransportLimits() TransportLimits {
	return TransportLimits{
		DNSAndDialTimeout:     5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		TotalTimeout:          30 * time.Second,
		MaxRequestBytes:       1 << 20,
		MaxResponseBytes:      8 << 20,
		MaxRedirects:          3,
		MaxRetries:            1,
	}
}

// LookupIPFunc resolves a hostname for destination policy checks.
type LookupIPFunc func(context.Context, string) ([]net.IPAddr, error)

// TransportConfig configures the shared hardened provider client.
type TransportConfig struct {
	Limits              TransportLimits
	AllowPrivateNetwork bool
	LookupIP            LookupIPFunc
	Base                http.RoundTripper
}

// NewHTTPClient creates the shared cancellable, bounded, SSRF-aware provider client.
func NewHTTPClient(config TransportConfig) *http.Client {
	limits := withDefaultTransportLimits(config.Limits)
	lookup := config.LookupIP
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	base := config.Base
	if base == nil {
		dialer := &net.Dialer{Timeout: limits.DNSAndDialTimeout, KeepAlive: 30 * time.Second}
		base = &http.Transport{
			DialContext:           policyDialContext(dialer, lookup, config.AllowPrivateNetwork),
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          20,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   limits.DNSAndDialTimeout,
			ResponseHeaderTimeout: limits.ResponseHeaderTimeout,
		}
	}
	client := &http.Client{
		Transport: &policyRoundTripper{
			base: base, limits: limits, lookup: lookup, allowPrivateNetwork: config.AllowPrivateNetwork,
		},
		Timeout: limits.TotalTimeout,
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > limits.MaxRedirects {
			return errors.New("AI provider redirect limit exceeded")
		}
		return validateRequestDestination(request.Context(), request.URL.String(), lookup, config.AllowPrivateNetwork)
	}
	return client
}

func withDefaultTransportLimits(limits TransportLimits) TransportLimits {
	defaults := DefaultTransportLimits()
	if limits.DNSAndDialTimeout <= 0 {
		limits.DNSAndDialTimeout = defaults.DNSAndDialTimeout
	}
	if limits.ResponseHeaderTimeout <= 0 {
		limits.ResponseHeaderTimeout = defaults.ResponseHeaderTimeout
	}
	if limits.TotalTimeout <= 0 {
		limits.TotalTimeout = defaults.TotalTimeout
	}
	if limits.MaxRequestBytes <= 0 {
		limits.MaxRequestBytes = defaults.MaxRequestBytes
	}
	if limits.MaxResponseBytes <= 0 {
		limits.MaxResponseBytes = defaults.MaxResponseBytes
	}
	if limits.MaxRedirects <= 0 {
		limits.MaxRedirects = defaults.MaxRedirects
	}
	if limits.MaxRetries < 0 {
		limits.MaxRetries = 0
	}
	if limits.MaxRetries == 0 {
		limits.MaxRetries = defaults.MaxRetries
	}
	if limits.MaxRetries > 2 {
		limits.MaxRetries = 2
	}
	return limits
}

type policyRoundTripper struct {
	base                http.RoundTripper
	limits              TransportLimits
	lookup              LookupIPFunc
	allowPrivateNetwork bool
}

func (transport *policyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := validateRequestDestination(request.Context(), request.URL.String(), transport.lookup, transport.allowPrivateNetwork); err != nil {
		return nil, err
	}
	if request.ContentLength > transport.limits.MaxRequestBytes {
		return nil, errors.Errorf("AI provider request exceeds %d bytes", transport.limits.MaxRequestBytes)
	}
	if request.Body != nil && request.ContentLength < 0 {
		request.Body = &limitedReadCloser{reader: request.Body, closer: request.Body, remaining: transport.limits.MaxRequestBytes, label: "request"}
	}
	response, err := transport.roundTripWithRetry(request)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > transport.limits.MaxResponseBytes {
		_ = response.Body.Close()
		return nil, errors.Errorf("AI provider response exceeds %d bytes", transport.limits.MaxResponseBytes)
	}
	response.Body = &limitedReadCloser{reader: response.Body, closer: response.Body, remaining: transport.limits.MaxResponseBytes, label: "response"}
	return response, nil
}

func (transport *policyRoundTripper) roundTripWithRetry(request *http.Request) (*http.Response, error) {
	current := request
	for attempt := 0; ; attempt++ {
		response, err := transport.base.RoundTrip(current)
		retryable := err != nil || (response != nil && isRetryableStatus(response.StatusCode))
		if !retryable || attempt >= transport.limits.MaxRetries || request.Context().Err() != nil {
			return response, err
		}
		if request.Body != nil && request.GetBody == nil {
			return response, err
		}
		delay := 50 * time.Millisecond
		if response != nil {
			delay = ParseRetryAfter(response.Header.Get("Retry-After"))
			if delay == 0 {
				delay = 50 * time.Millisecond
			}
			_ = response.Body.Close()
		}
		timer := time.NewTimer(delay)
		select {
		case <-request.Context().Done():
			timer.Stop()
			return nil, request.Context().Err()
		case <-timer.C:
		}
		current = request.Clone(request.Context())
		if request.GetBody != nil {
			body, bodyErr := request.GetBody()
			if bodyErr != nil {
				return nil, errors.Wrap(bodyErr, "failed to retry AI provider request")
			}
			current.Body = body
		}
	}
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusBadGateway ||
		statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout
}

type limitedReadCloser struct {
	reader    io.Reader
	closer    io.Closer
	remaining int64
	label     string
}

func (reader *limitedReadCloser) Read(buffer []byte) (int, error) {
	if reader.remaining == 0 {
		var probe [1]byte
		n, err := reader.reader.Read(probe[:])
		if n > 0 {
			return 0, errors.Errorf("AI provider %s exceeds configured size limit", reader.label)
		}
		return 0, err
	}
	if int64(len(buffer)) > reader.remaining {
		buffer = buffer[:reader.remaining]
	}
	n, err := reader.reader.Read(buffer)
	reader.remaining -= int64(n)
	return n, err
}

func (reader *limitedReadCloser) Close() error { return reader.closer.Close() }

func validateRequestDestination(ctx context.Context, rawURL string, lookup LookupIPFunc, allowPrivateNetwork bool) error {
	parsed, err := ValidateEndpoint(rawURL, allowPrivateNetwork)
	if err != nil {
		return err
	}
	if allowPrivateNetwork {
		return nil
	}
	addresses, err := resolveHost(ctx, parsed.Hostname(), lookup)
	if err != nil {
		return errors.Wrap(err, "failed to resolve AI provider endpoint")
	}
	for _, address := range addresses {
		if isPrivateDestination(address.IP) {
			return errors.New("AI provider endpoint requires explicit private-network authorization")
		}
	}
	return nil
}

func resolveHost(ctx context.Context, host string, lookup LookupIPFunc) ([]net.IPAddr, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IPAddr{{IP: ip}}, nil
	}
	addresses, err := lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("AI provider endpoint resolved to no addresses")
	}
	return addresses, nil
}

func policyDialContext(dialer *net.Dialer, lookup LookupIPFunc, allowPrivateNetwork bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.Wrap(err, "invalid AI provider dial address")
		}
		addresses, err := resolveHost(ctx, host, lookup)
		if err != nil {
			return nil, errors.Wrap(err, "failed to resolve AI provider endpoint")
		}
		for _, resolved := range addresses {
			if !allowPrivateNetwork && isPrivateDestination(resolved.IP) {
				return nil, errors.New("AI provider endpoint requires explicit private-network authorization")
			}
		}
		var lastErr error
		for _, resolved := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
			if err == nil {
				return connection, nil
			}
			lastErr = err
		}
		return nil, errors.Wrap(lastErr, "AI provider connection failed")
	}
}

// EndpointWithPath appends provider API path segments without changing endpoint query policy.
func EndpointWithPath(endpoint string, pathSegments ...string) (string, error) {
	parsed, err := ValidateEndpoint(endpoint, true)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimSuffix(parsed.Path, "/")
	for _, segment := range pathSegments {
		basePath += "/" + strings.Trim(segment, "/")
	}
	parsed.Path = basePath
	return parsed.String(), nil
}

// ParseRetryAfter returns a bounded Retry-After delay.
func ParseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	delay := time.Duration(seconds) * time.Second
	if delay > 2*time.Second {
		return 2 * time.Second
	}
	return delay
}
