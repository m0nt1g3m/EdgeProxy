package cli

import (
	"strings"

	"EdgeProxy/internal/nginx"

	"github.com/charmbracelet/huh"
)

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

	domainList := strings.Split(domains, ",")
	for i := range domainList {
		domainList[i] = strings.TrimSpace(domainList[i])
	}
	primary := domainList[0]
	upstreamName := "up_" + strings.NewReplacer(".", "_", "-", "_").Replace(primary)

	data := nginx.ConfigData{
		Domains:      strings.Join(domainList, ","),
		ServerNames:  strings.Join(domainList, " "),
		Primary:      primary,
		Type:         kind,
		Upstream:     upstream,
		UpstreamName: upstreamName,
		RedirectTo:   redirectTo,
		BodySize:     bodySize,
		SSL:          ssl,
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
