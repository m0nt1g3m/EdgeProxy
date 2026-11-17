package nginx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigRoundTripsGeneratedSite(t *testing.T) {
	confDir := t.TempDir()
	original := ConfigData{
		Domains: []string{"example.com", "www.example.com"},
		Type:    TypeProxy,
		Servers: []Server{
			{Address: "app:8080", MaxFails: 3, FailTimeout: "10s", Resolve: true},
			{Address: "host.docker.internal:8081", Weight: 2, MaxFails: 5, FailTimeout: "30s", Backup: true},
		},
		BodySize:          "100M",
		SSL:               true,
		Balance:           BalanceLeastConn,
		Keepalive:         64,
		ConnectTimeout:    "5s",
		ReadTimeout:       "40s",
		SendTimeout:       "50s",
		NextUpstreamTries: 4,
		RateLimit:         10,
		RateBurst:         20,
		ConnLimit:         50,
		CacheTTL:          "30s",
	}

	confPath, err := GenerateAndSave(confDir, original)
	if err != nil {
		t.Fatalf("GenerateAndSave: %v", err)
	}

	parsed, err := ParseConfig(confPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	if len(parsed.Domains) != 2 || parsed.Domains[0] != "example.com" || parsed.Domains[1] != "www.example.com" {
		t.Fatalf("domains = %v", parsed.Domains)
	}
	if parsed.Type != TypeProxy || !parsed.SSL {
		t.Fatalf("type = %q, ssl = %v", parsed.Type, parsed.SSL)
	}
	if parsed.Balance != BalanceLeastConn {
		t.Fatalf("balance = %q", parsed.Balance)
	}
	if len(parsed.Servers) != 2 {
		t.Fatalf("servers = %v", parsed.Servers)
	}
	if parsed.Servers[0].Address != "app:8080" || !parsed.Servers[0].Resolve {
		t.Fatalf("first server = %+v", parsed.Servers[0])
	}
	if parsed.Servers[1].Weight != 2 || !parsed.Servers[1].Backup {
		t.Fatalf("second server = %+v", parsed.Servers[1])
	}
	if parsed.Keepalive != 64 {
		t.Fatalf("keepalive = %d", parsed.Keepalive)
	}
	if parsed.ConnectTimeout != "5s" || parsed.ReadTimeout != "40s" || parsed.SendTimeout != "50s" {
		t.Fatalf("timeouts = %q/%q/%q", parsed.ConnectTimeout, parsed.ReadTimeout, parsed.SendTimeout)
	}
	if parsed.NextUpstreamTries != 4 {
		t.Fatalf("tries = %d", parsed.NextUpstreamTries)
	}
	if parsed.RateLimit != 10 || parsed.RateBurst != 20 || parsed.ConnLimit != 50 {
		t.Fatalf("limits = %d/%d/%d", parsed.RateLimit, parsed.RateBurst, parsed.ConnLimit)
	}
	if parsed.CacheTTL != "30s" {
		t.Fatalf("cache ttl = %q", parsed.CacheTTL)
	}
}

func TestParseConfigImportsLegacySite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.conf")
	legacy := `upstream up_legacy_example_com {
    zone up_legacy_example_com 64k;
    least_conn;
    server host.docker.internal:8080;
    keepalive 64;
    resolver 127.0.0.11 valid=10s ipv6=off;
}

server {
    listen 80;
    listen [::]:80;
    server_name legacy.example.com;

    location /.well-known/acme-challenge/ { root /var/www/certbot; }

    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name legacy.example.com;

    ssl_certificate     /etc/letsencrypt/live/legacy.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/legacy.example.com/privkey.pem;

    client_max_body_size 10M;
    location / {
        proxy_pass http://up_legacy_example_com;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }
}
`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	parsed, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	if !parsed.SSL || parsed.Type != TypeProxy {
		t.Fatalf("type = %q, ssl = %v", parsed.Type, parsed.SSL)
	}
	if len(parsed.Servers) != 1 || parsed.Servers[0].Address != "host.docker.internal:8080" {
		t.Fatalf("servers = %v", parsed.Servers)
	}
	if parsed.Balance != BalanceLeastConn || parsed.Keepalive != 64 {
		t.Fatalf("balance/keepalive = %q/%d", parsed.Balance, parsed.Keepalive)
	}
	if parsed.ReadTimeout != DefaultReadTimeout {
		t.Fatalf("read timeout = %q, want default %q", parsed.ReadTimeout, DefaultReadTimeout)
	}
}

func TestParseConfigImportsRedirect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "redirect.conf")
	redirect := `server {
    listen 80;
    listen [::]:80;
    server_name www.example.com;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://example.com$request_uri; }
}
`
	if err := os.WriteFile(path, []byte(redirect), 0o644); err != nil {
		t.Fatalf("write redirect config: %v", err)
	}

	parsed, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if parsed.Type != TypeRedirect || parsed.RedirectTo != "https://example.com" {
		t.Fatalf("type/redirect = %q/%q", parsed.Type, parsed.RedirectTo)
	}
	if parsed.SSL {
		t.Fatal("plain HTTP redirect must not be marked as SSL")
	}
}
