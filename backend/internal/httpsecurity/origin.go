// Package httpsecurity centralizes browser-facing HTTP security decisions.
package httpsecurity

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// RequestHostIsLoopback rejects DNS-rebinding hostnames on an auth-disabled
// listener. Binding the socket to loopback is not sufficient: a hostile DNS
// name can resolve to 127.0.0.1 and make the browser regard that name as the
// request's same origin.
func RequestHostIsLoopback(r *http.Request) bool {
	host := r.Host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	} else {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// OriginAllowed reports whether a browser Origin is either explicitly
// allowlisted or exactly matches the request's own scheme and host. Requests
// without Origin are non-browser or same-origin requests where the browser did
// not need to send the header, so they remain allowed.
func OriginAllowed(r *http.Request, allowed map[string]bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if allowed[origin] {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	wantScheme := "http"
	if r.TLS != nil {
		wantScheme = "https"
	}
	return strings.EqualFold(u.Scheme, wantScheme) && strings.EqualFold(u.Host, r.Host)
}

// RequestsBearer reports whether the request explicitly presents a bearer
// credential. Authentication still validates the credential later. This only
// distinguishes explicit authority from ambient cookies for CORS handling.
func RequestsBearer(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	return strings.HasPrefix(h, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")) != ""
}

// PreflightRequestsBearer reports whether a CORS preflight asks permission to
// send an Authorization header.
func PreflightRequestsBearer(r *http.Request) bool {
	for header := range strings.SplitSeq(r.Header.Get("Access-Control-Request-Headers"), ",") {
		if strings.EqualFold(strings.TrimSpace(header), "authorization") {
			return true
		}
	}
	return false
}
