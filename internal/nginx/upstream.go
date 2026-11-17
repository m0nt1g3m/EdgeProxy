package nginx

import (
	"net"
	"strings"
)

// NormalizeUpstream prepares a user supplied upstream for use by nginx inside
// the EdgeProxy container.
//
// Loopback targets are reachable on the host, not inside the nginx container,
// so they are rewritten to host.docker.internal. relayPort is set when the
// deployer needs to expose that host port to containers.
//
// resolve reports whether the upstream host must be resolved through Docker's
// embedded DNS (for example another container name). Static addresses, such as
// IP literals and host.docker.internal, are resolved through /etc/hosts instead.
func NormalizeUpstream(raw string) (upstream, relayPort string, resolve bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimSuffix(raw, "/")

	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return raw, "", !isStaticHost(raw)
	}

	normalizedHost := strings.ToLower(strings.Trim(host, "[]"))

	switch normalizedHost {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return "host.docker.internal:" + port, port, false
	case "host.docker.internal":
		return raw, "", false
	}

	return raw, "", !isStaticHost(normalizedHost)
}

func isStaticHost(host string) bool {
	if strings.EqualFold(host, "host.docker.internal") {
		return true
	}

	return net.ParseIP(strings.Trim(host, "[]")) != nil
}
