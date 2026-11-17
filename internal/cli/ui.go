package cli

import (
	"fmt"
	"net/url"
	"strings"

	"EdgeProxy/internal/nginx"

	"github.com/charmbracelet/huh"
)

// BuildConfig turns raw values from the interactive form or command line flags
// into nginx configuration data.
func BuildConfig(domains, kind, upstream, redirectTo, bodySize string, ssl bool) (nginx.ConfigData, error) {
	domainList := strings.Split(domains, ",")
	for i := range domainList {
		domainList[i] = strings.TrimSpace(domainList[i])
	}
	domainList = removeEmpty(domainList)
	if len(domainList) == 0 {
		return nginx.ConfigData{}, fmt.Errorf("at least one domain is required")
	}
	for _, domain := range domainList {
		if !validDomain(domain) {
			return nginx.ConfigData{}, fmt.Errorf("invalid domain %q", domain)
		}
	}

	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "proxy"
	}

	switch kind {
	case "proxy", "websocket", "stream":
		if strings.TrimSpace(upstream) == "" {
			return nginx.ConfigData{}, fmt.Errorf("upstream host:port is required for %s", kind)
		}
	case "redirect":
		if strings.TrimSpace(redirectTo) == "" {
			return nginx.ConfigData{}, fmt.Errorf("redirect target is required for redirect")
		}
		target, err := url.Parse(redirectTo)
		if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
			return nginx.ConfigData{}, fmt.Errorf("redirect target must be an absolute http(s) URL")
		}
	default:
		return nginx.ConfigData{}, fmt.Errorf("unknown service type %q", kind)
	}

	if strings.TrimSpace(bodySize) == "" {
		bodySize = "10M"
	}

	primary := domainList[0]
	upstreamName := "up_" + strings.NewReplacer(".", "_", "-", "_").Replace(primary)
	resolvedUpstream, relayPort, resolve := nginx.NormalizeUpstream(upstream)

	data := nginx.ConfigData{
		Domains:      strings.Join(domainList, ","),
		ServerNames:  strings.Join(domainList, " "),
		Primary:      primary,
		Type:         kind,
		Upstream:     resolvedUpstream,
		UpstreamName: upstreamName,
		RedirectTo:   redirectTo,
		BodySize:     bodySize,
		SSL:          ssl,
		Resolve:      resolve,
		RelayPort:    relayPort,
		ProxyHeaders: `
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;`,
	}

	if kind == "websocket" {
		data.ExtraConfig = `
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;`
	} else if kind == "stream" {
		data.ExtraConfig = `
        proxy_buffering         off;
        proxy_request_buffering off;`
		data.BodySize = "0"
	}

	return data, nil
}

func removeEmpty(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func validDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}

	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		for i, r := range label {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '-' && i != 0 && i != len(label)-1:
			default:
				return false
			}
		}
	}

	return true
}

func RunInteractive() (nginx.ConfigData, error) {
	var (
		domains    string
		kind       string
		upstream   string
		redirectTo string
		ssl        bool
		bodySize   = "10M"
	)

	form1 := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Domain(s)").
				Description("Comma-separated (e.g. example.com,www.example.com)").
				Placeholder("grippy.work.gd").
				Value(&domains),
			huh.NewSelect[string]().
				Title("Service Type").
				Options(
					huh.NewOption("Standard reverse proxy", "proxy"),
					huh.NewOption("WebSocket", "websocket"),
					huh.NewOption("Streaming (no buffering)", "stream"),
					huh.NewOption("Redirect", "redirect"),
				).
				Value(&kind),
		),
	)

	if err := form1.Run(); err != nil {
		return nginx.ConfigData{}, err
	}

	if kind == "redirect" {
		form2 := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Redirect Target").Placeholder("https://example.com").Value(&redirectTo),
		))
		if err := form2.Run(); err != nil {
			return nginx.ConfigData{}, err
		}
	} else {
		form2 := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Upstream host:port").Placeholder("service:8080").Value(&upstream),
			huh.NewInput().Title("Max body size").Value(&bodySize),
		))
		if err := form2.Run(); err != nil {
			return nginx.ConfigData{}, err
		}
	}

	form3 := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Issue Let's Encrypt certificate?").Value(&ssl),
	))
	if err := form3.Run(); err != nil {
		return nginx.ConfigData{}, err
	}

	return BuildConfig(domains, kind, upstream, redirectTo, bodySize, ssl)
}
