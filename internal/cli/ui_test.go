package cli

import (
	"strings"
	"testing"

	"EdgeProxy/internal/nginx"
)

func TestBuildConfigNormalizesLoopbackUpstream(t *testing.T) {
	data, err := BuildConfig(Input{
		Domains:   "proxy.dondegusto.ru",
		Type:      "proxy",
		Upstreams: "127.0.0.1:8080",
		SSL:       true,
	})
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	if len(data.Servers) != 1 {
		t.Fatalf("servers = %v", data.Servers)
	}
	if data.Servers[0].Address != "host.docker.internal:8080" {
		t.Fatalf("upstream = %q, want host.docker.internal:8080", data.Servers[0].Address)
	}
	if data.Servers[0].RelayPort != "8080" {
		t.Fatalf("relay port = %q, want 8080", data.Servers[0].RelayPort)
	}
	if data.Servers[0].Resolve {
		t.Fatal("host.docker.internal must not use Docker DNS resolution")
	}
	if data.UpstreamName() != "up_proxy_dondegusto_ru" {
		t.Fatalf("unexpected upstream name: %q", data.UpstreamName())
	}
}

func TestBuildConfigSupportsMultipleUpstreams(t *testing.T) {
	data, err := BuildConfig(Input{
		Domains:   "api.example.com",
		Type:      "proxy",
		Upstreams: "app1:8080, app2:8080 weight=2, 127.0.0.1:9000 backup",
	})
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	if len(data.Servers) != 3 {
		t.Fatalf("servers = %v", data.Servers)
	}
	if data.Balance != nginx.BalanceLeastConn {
		t.Fatalf("balance = %q", data.Balance)
	}
	if data.Servers[1].Weight != 2 || !data.Servers[2].Backup {
		t.Fatalf("servers = %+v", data.Servers)
	}
	if ports := data.RelayPorts(); len(ports) != 1 || ports[0] != "9000" {
		t.Fatalf("relay ports = %v", ports)
	}
}

func TestBuildConfigStreamDisablesBuffering(t *testing.T) {
	data, err := BuildConfig(Input{
		Domains:   "s3.dondegusto.ru",
		Type:      "stream",
		Upstreams: "127.0.0.1:18080",
		SSL:       true,
	})
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	if data.BodySize != "0" {
		t.Fatalf("body size = %q, want 0", data.BodySize)
	}
	if data.ReadTimeout != nginx.DefaultStreamReadTimeout {
		t.Fatalf("read timeout = %q, want %q", data.ReadTimeout, nginx.DefaultStreamReadTimeout)
	}
}

func TestBuildConfigAppliesResilienceDefaults(t *testing.T) {
	data, err := BuildConfig(Input{
		Domains:   "example.com",
		Upstreams: "app:8080",
	})
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	if data.ConnectTimeout != nginx.DefaultConnectTimeout ||
		data.ReadTimeout != nginx.DefaultReadTimeout ||
		data.SendTimeout != nginx.DefaultSendTimeout {
		t.Fatalf("timeouts = %q/%q/%q", data.ConnectTimeout, data.ReadTimeout, data.SendTimeout)
	}
	if data.NextUpstreamTries != nginx.DefaultNextUpstreamTries {
		t.Fatalf("tries = %d", data.NextUpstreamTries)
	}
	if data.Keepalive != nginx.DefaultKeepalive {
		t.Fatalf("keepalive = %d", data.Keepalive)
	}
}

func TestBuildConfigRateLimitDefaultBurst(t *testing.T) {
	data, err := BuildConfig(Input{
		Domains:   "example.com",
		Upstreams: "app:8080",
		RateLimit: "10",
	})
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}
	if data.RateBurst != nginx.DefaultRateBurst {
		t.Fatalf("burst = %d, want %d", data.RateBurst, nginx.DefaultRateBurst)
	}
}

func TestBuildConfigRejectsBackupWithIncompatibleBalance(t *testing.T) {
	_, err := BuildConfig(Input{
		Domains:   "example.com",
		Upstreams: "app1:8080, app2:8080 backup",
		Balance:   nginx.BalanceIPHash,
	})
	if err == nil {
		t.Fatal("expected backup + ip_hash to be rejected")
	}
}

func TestBuildConfigRejectsCacheForWebSocket(t *testing.T) {
	_, err := BuildConfig(Input{
		Domains:   "example.com",
		Type:      "websocket",
		Upstreams: "app:8080",
		CacheTTL:  "30s",
	})
	if err == nil {
		t.Fatal("expected cache on websocket site to be rejected")
	}
}

func TestBuildConfigRequiresUpstream(t *testing.T) {
	if _, err := BuildConfig(Input{Domains: "example.com", Type: "proxy"}); err == nil {
		t.Fatal("expected error for missing upstream")
	}
}

func TestBuildConfigRequiresRedirectTarget(t *testing.T) {
	if _, err := BuildConfig(Input{Domains: "www.example.com", Type: "redirect"}); err == nil {
		t.Fatal("expected error for missing redirect target")
	}
}

func TestBuildConfigRejectsUnknownType(t *testing.T) {
	if _, err := BuildConfig(Input{Domains: "example.com", Type: "unknown", Upstreams: "app:8080"}); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestBuildConfigRejectsInvalidDomain(t *testing.T) {
	for _, domain := range []string{
		"example.com; touch /tmp/pwned",
		"exa mple.com",
		"-example.com",
		"example..com",
	} {
		if _, err := BuildConfig(Input{Domains: domain, Upstreams: "app:8080"}); err == nil {
			t.Fatalf("expected error for domain %q", domain)
		}
	}
}

func TestBuildConfigRejectsRelativeRedirectTarget(t *testing.T) {
	if _, err := BuildConfig(Input{Domains: "www.example.com", Type: "redirect", RedirectTo: "/login"}); err == nil {
		t.Fatal("expected error for relative redirect target")
	}
}

func TestBuildConfigNormalizesDomainsAndRedirect(t *testing.T) {
	data, err := BuildConfig(Input{
		Domains:    "WWW.Example.com, example.com, www.example.com",
		Type:       "redirect",
		RedirectTo: "https://example.com/",
		SSL:        true,
	})
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	if len(data.Domains) != 2 || data.Domains[0] != "www.example.com" || data.Domains[1] != "example.com" {
		t.Fatalf("domains = %v", data.Domains)
	}
	if data.RedirectTo != "https://example.com" {
		t.Fatalf("redirect = %q", data.RedirectTo)
	}
}

func TestInputFromConfigRoundTrip(t *testing.T) {
	original := nginx.ConfigData{
		Domains:            []string{"example.com"},
		Type:               nginx.TypeProxy,
		Servers:            []nginx.Server{{Address: "app:8080", MaxFails: 3, FailTimeout: "10s", Resolve: true}},
		BodySize:           "10M",
		Balance:            nginx.BalanceLeastConn,
		Keepalive:          64,
		ConnectTimeout:     "5s",
		ReadTimeout:        "40s",
		SendTimeout:        "60s",
		NextUpstreamTries:  3,
		NextUpstreamWindow: "10s",
		RateLimit:          10,
		RateBurst:          20,
		ConnLimit:          5,
		CacheTTL:           "30s",
	}

	input := InputFromConfig(original)
	rebuilt, err := BuildConfig(input)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	if rebuilt.UpstreamInput() != original.UpstreamInput() {
		t.Fatalf("upstreams = %q, want %q", rebuilt.UpstreamInput(), original.UpstreamInput())
	}
	if rebuilt.RateLimit != 10 || rebuilt.RateBurst != 20 || rebuilt.ConnLimit != 5 {
		t.Fatalf("limits = %d/%d/%d", rebuilt.RateLimit, rebuilt.RateBurst, rebuilt.ConnLimit)
	}
	if rebuilt.CacheTTL != "30s" {
		t.Fatalf("cache = %q", rebuilt.CacheTTL)
	}
}

func TestUpstreamFieldAcceptsCompactSyntax(t *testing.T) {
	servers, err := nginx.ParseServers("app1:8080, app2:8080 weight=2 backup")
	if err != nil {
		t.Fatalf("ParseServers: %v", err)
	}
	if len(servers) != 2 || !strings.Contains(servers[1].Directive(), "backup") {
		t.Fatalf("servers = %+v", servers)
	}
}
