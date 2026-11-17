package nginx

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	reServerName    = regexp.MustCompile(`(?m)^\s*server_name\s+([^;]+);`)
	reRedirect      = regexp.MustCompile(`return\s+301\s+([^\s;$]+)\$request_uri`)
	reUpstreamBlock = regexp.MustCompile(`(?ms)^upstream\s+\S+\s*\{(.*?)^\}`)
	reUpstreamServe = regexp.MustCompile(`(?m)^\s*server\s+([^;]+);`)
	reKeepalive     = regexp.MustCompile(`(?m)^\s*keepalive\s+(\d+);`)
	reBodySize      = regexp.MustCompile(`(?m)^\s*client_max_body_size\s+([^;]+);`)
	reConnectTO     = regexp.MustCompile(`(?m)^\s*proxy_connect_timeout\s+([^;]+);`)
	reReadTO        = regexp.MustCompile(`(?m)^\s*proxy_read_timeout\s+([^;]+);`)
	reSendTO        = regexp.MustCompile(`(?m)^\s*proxy_send_timeout\s+([^;]+);`)
	reNextTries     = regexp.MustCompile(`(?m)^\s*proxy_next_upstream_tries\s+(\d+);`)
	reNextWindow    = regexp.MustCompile(`(?m)^\s*proxy_next_upstream_timeout\s+([^;]+);`)
	reRateLimit     = regexp.MustCompile(`rate=(\d+)r/s`)
	reRateBurst     = regexp.MustCompile(`burst=(\d+)`)
	reConnLimit     = regexp.MustCompile(`(?m)^\s*limit_conn\s+\S+\s+(\d+);`)
	reCacheEnabled  = regexp.MustCompile(`(?m)^\s*proxy_cache\s+\S+;`)
	reCacheTTL      = regexp.MustCompile(`proxy_cache_valid\s+200\s+301\s+302\s+([^;]+);`)
	reSSLListen     = regexp.MustCompile(`(?m)^\s*listen\s+[^;]*\b443\b[^;]*\bssl\b[^;]*;`)
	reUpgrade       = regexp.MustCompile(`proxy_set_header\s+Upgrade`)
	reBufferOff     = regexp.MustCompile(`proxy_buffering\s+off;`)
	reRequestBuffer = regexp.MustCompile(`proxy_request_buffering\s+off;`)
)

// ParseConfig reads a generated nginx site file back into ConfigData. It is a
// best-effort importer for configs that were created before site metadata
// existed, or by hand using the same conventions.
func ParseConfig(path string) (ConfigData, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ConfigData{}, err
	}

	text := string(content)
	data := ConfigData{}

	match := reServerName.FindStringSubmatch(text)
	if match == nil {
		return ConfigData{}, fmt.Errorf("no server_name directive found in %s", path)
	}
	for _, domain := range strings.Fields(match[1]) {
		if domain == "_" {
			continue
		}
		data.Domains = append(data.Domains, domain)
	}
	if len(data.Domains) == 0 {
		return ConfigData{}, fmt.Errorf("no usable server_name directive found in %s", path)
	}

	data.SSL = reSSLListen.MatchString(text)

	if match := reRedirect.FindStringSubmatch(text); match != nil {
		data.Type = TypeRedirect
		data.RedirectTo = match[1]
	}

	if match := reUpstreamBlock.FindStringSubmatch(text); match != nil {
		body := match[1]
		data.Type = TypeProxy
		data.Balance = parseBalance(body)

		if match := reKeepalive.FindStringSubmatch(body); match != nil {
			data.Keepalive, _ = strconv.Atoi(match[1])
		}

		for _, serverMatch := range reUpstreamServe.FindAllStringSubmatch(body, -1) {
			server, err := parseServerDirective(serverMatch[1])
			if err != nil {
				return ConfigData{}, fmt.Errorf("parse upstream server in %s: %w", path, err)
			}
			data.Servers = append(data.Servers, server)
		}

		if reUpgrade.MatchString(text) {
			data.Type = TypeWebSocket
		} else if reBufferOff.MatchString(text) && reRequestBuffer.MatchString(text) {
			data.Type = TypeStream
		}
	}

	if data.Type == "" {
		return ConfigData{}, fmt.Errorf("could not detect site type in %s", path)
	}

	if match := reBodySize.FindStringSubmatch(text); match != nil {
		data.BodySize = strings.TrimSpace(match[1])
	}
	if match := reConnectTO.FindStringSubmatch(text); match != nil {
		data.ConnectTimeout = strings.TrimSpace(match[1])
	}
	if match := reReadTO.FindStringSubmatch(text); match != nil {
		data.ReadTimeout = strings.TrimSpace(match[1])
	}
	if match := reSendTO.FindStringSubmatch(text); match != nil {
		data.SendTimeout = strings.TrimSpace(match[1])
	}
	if match := reNextTries.FindStringSubmatch(text); match != nil {
		data.NextUpstreamTries, _ = strconv.Atoi(match[1])
	}
	if match := reNextWindow.FindStringSubmatch(text); match != nil {
		data.NextUpstreamWindow = strings.TrimSpace(match[1])
	}
	if match := reRateLimit.FindStringSubmatch(text); match != nil {
		data.RateLimit, _ = strconv.Atoi(match[1])
	}
	if match := reRateBurst.FindStringSubmatch(text); match != nil {
		data.RateBurst, _ = strconv.Atoi(match[1])
	}
	if match := reConnLimit.FindStringSubmatch(text); match != nil {
		data.ConnLimit, _ = strconv.Atoi(match[1])
	}
	if reCacheEnabled.MatchString(text) {
		if match := reCacheTTL.FindStringSubmatch(text); match != nil {
			data.CacheTTL = strings.TrimSpace(match[1])
		}
	}

	data.ApplyDefaults()
	return data, nil
}

func parseBalance(upstreamBody string) string {
	switch {
	case strings.Contains(upstreamBody, "random two least_conn;"):
		return BalanceRandomTwo
	case strings.Contains(upstreamBody, "hash $request_uri consistent;"):
		return BalanceHashURI
	case strings.Contains(upstreamBody, "ip_hash;"):
		return BalanceIPHash
	case strings.Contains(upstreamBody, "least_conn;"):
		return BalanceLeastConn
	default:
		return BalanceRoundRobin
	}
}

func parseServerDirective(value string) (Server, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return Server{}, fmt.Errorf("empty server directive")
	}

	server := Server{
		Address:     fields[0],
		MaxFails:    DefaultMaxFails,
		FailTimeout: DefaultFailTimeout,
	}

	for _, option := range fields[1:] {
		key, value, hasValue := strings.Cut(option, "=")
		switch key {
		case "weight":
			weight, err := strconv.Atoi(value)
			if err != nil || weight < 1 {
				return Server{}, fmt.Errorf("invalid weight %q", value)
			}
			server.Weight = weight
		case "max_fails":
			maxFails, err := strconv.Atoi(value)
			if err != nil || maxFails < 0 {
				return Server{}, fmt.Errorf("invalid max_fails %q", value)
			}
			server.MaxFails = maxFails
		case "fail_timeout":
			if !hasValue || !ValidDuration(value) {
				return Server{}, fmt.Errorf("invalid fail_timeout %q", value)
			}
			server.FailTimeout = value
		case "backup":
			server.Backup = true
		case "down":
			server.Down = true
		case "resolve":
			server.Resolve = true
		default:
			return Server{}, fmt.Errorf("unknown server option %q", option)
		}
	}

	return server, nil
}
