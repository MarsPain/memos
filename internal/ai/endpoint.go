package ai

import (
	"net"
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

var secretQueryNames = map[string]struct{}{
	"api_key": {}, "apikey": {}, "key": {}, "token": {}, "access_token": {}, "authorization": {}, "secret": {}, "password": {},
}

// ValidateEndpoint validates provider endpoint syntax and literal destination policy.
func ValidateEndpoint(rawURL string, allowPrivateNetwork bool) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.Wrap(err, "invalid AI provider endpoint")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("AI provider endpoint must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return nil, errors.New("AI provider endpoint must not contain user information")
	}
	if parsed.Fragment != "" {
		return nil, errors.New("AI provider endpoint must not contain a fragment")
	}
	if parsed.Hostname() == "" || strings.ContainsAny(parsed.Hostname(), " \t\r\n") {
		return nil, errors.New("AI provider endpoint requires a valid host")
	}
	for key := range parsed.Query() {
		if _, secret := secretQueryNames[strings.ToLower(key)]; secret {
			return nil, errors.New("AI provider endpoint must not contain secret-bearing query parameters")
		}
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" && !allowPrivateNetwork {
		return nil, errors.New("AI provider endpoint requires explicit private-network authorization")
	}
	if ip := net.ParseIP(host); ip != nil && !allowPrivateNetwork && isPrivateDestination(ip) {
		return nil, errors.New("AI provider endpoint requires explicit private-network authorization")
	}
	return parsed, nil
}

func isPrivateDestination(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
