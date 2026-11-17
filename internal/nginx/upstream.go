package nginx

import (
	"fmt"
	"net"
	"strconv"
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

// ParseServers turns a comma-separated upstream list into normalized backends.
//
// Each entry is "host:port" optionally followed by space-separated options:
//
//	weight=N        relative weight (default 1)
//	max_fails=N     failures before the server is ejected (default 3)
//	fail_timeout=T  ejection window (default 10s)
//	backup          only used when primary servers are unavailable
//	down            administratively disabled
//
// Example:
//
//	app1:8080, app2:8080 weight=2 max_fails=5, 127.0.0.1:9000 backup
func ParseServers(raw string) ([]Server, error) {
	var servers []Server

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		fields := strings.Fields(entry)
		if len(fields) == 0 {
			continue
		}

		address, relayPort, resolve := NormalizeUpstream(fields[0])
		if address == "" {
			return nil, fmt.Errorf("invalid upstream %q", fields[0])
		}

		server := Server{
			Address:     address,
			MaxFails:    DefaultMaxFails,
			FailTimeout: DefaultFailTimeout,
			Resolve:     resolve,
			RelayPort:   relayPort,
		}

		for _, option := range fields[1:] {
			key, value, hasValue := strings.Cut(option, "=")
			switch key {
			case "weight":
				weight, err := parsePositiveInt(value, hasValue, "weight")
				if err != nil {
					return nil, err
				}
				server.Weight = weight
			case "max_fails":
				maxFails, err := parseNonNegativeInt(value, hasValue, "max_fails")
				if err != nil {
					return nil, err
				}
				server.MaxFails = maxFails
			case "fail_timeout":
				if !hasValue || !ValidDuration(value) {
					return nil, fmt.Errorf("invalid fail_timeout %q for upstream %s", value, address)
				}
				server.FailTimeout = value
			case "backup":
				if hasValue {
					return nil, fmt.Errorf("backup does not take a value")
				}
				server.Backup = true
			case "down":
				if hasValue {
					return nil, fmt.Errorf("down does not take a value")
				}
				server.Down = true
			default:
				return nil, fmt.Errorf("unknown upstream option %q", option)
			}
		}

		servers = append(servers, server)
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("at least one upstream is required")
	}

	return servers, nil
}

func parsePositiveInt(value string, hasValue bool, name string) (int, error) {
	number, err := parseNonNegativeInt(value, hasValue, name)
	if err != nil {
		return 0, err
	}
	if number < 1 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return number, nil
}

func parseNonNegativeInt(value string, hasValue bool, name string) (int, error) {
	if !hasValue {
		return 0, fmt.Errorf("%s requires a value", name)
	}

	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return number, nil
}
