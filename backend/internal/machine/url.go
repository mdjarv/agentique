package machine

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// NormalizeRemoteBaseURL validates and canonicalizes the origin stored for a
// paired machine. Remote credentials require TLS. Plain HTTP is limited to
// loopback development, where it cannot cross the network in plaintext.
func NormalizeRemoteBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("baseUrl must be an absolute HTTP origin")
	}
	if u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("baseUrl must contain only scheme, host, and optional port")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", errors.New("baseUrl must use HTTPS")
	}
	if scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return "", errors.New("remote machines must use HTTPS")
	}
	return scheme + "://" + u.Host, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
