package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndSaveRendersFullProxyConfig(t *testing.T) {
	confDir := t.TempDir()

	data := ConfigData{
		Domains: []string{"example.com", "www.example.com"},
		Type:    TypeProxy,
		Servers: []Server{
			{Address: "app:8080", MaxFails: 3, FailTimeout: "10s", Resolve: true},
			{Address: "host.docker.internal:8081", Weight: 2, MaxFails: 5, FailTimeout: "30s", Backup: true},
		},
		BodySize:          "100m",
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

	confPath, err := GenerateAndSave(confDir, data)
	if err != nil {
		t.Fatalf("GenerateAndSave returned error: %v", err)
	}

	if confPath != filepath.Join(confDir, "example.com.conf") {
		t.Fatalf("unexpected config path: %s", confPath)
	}

	content, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	got := string(content)
	for _, want := range []string{
		"server_name example.com www.example.com;",
		"upstream up_example_com {",
		"least_conn;",
		"server app:8080 max_fails=3 fail_timeout=10s resolve;",
		"server host.docker.internal:8081 weight=2 max_fails=5 fail_timeout=30s backup;",
		"keepalive 64;",
		"proxy_pass http://up_example_com;",
		"proxy_next_upstream error timeout http_502 http_503 http_504;",
		"proxy_next_upstream_tries 4;",
		"limit_req zone=up_example_com_req burst=20 nodelay;",
		"limit_conn up_example_com_conn 50;",
		"proxy_cache up_example_com_cache;",
		"proxy_cache_valid 200 301 302 30s;",
		"ssl_certificate     /etc/letsencrypt/live/example.com/fullchain.pem;",
		"ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated config missing %q\n%s", want, got)
		}
	}

	if strings.Contains(got, "reuseport") {
		t.Fatalf("reuseport must not be used: nginx rejects it in more than one server block\n%s", got)
	}
}

func TestGenerateAndSaveFailsForMissingDirectory(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing", "conf")

	_, err := GenerateAndSave(missingDir, ConfigData{Domains: []string{"example.com"}})
	if err == nil {
		t.Fatal("expected error when target directory does not exist")
	}
}

func TestGeneratedConfigSupportsResolveAndRoundRobin(t *testing.T) {
	confDir := t.TempDir()

	data := ConfigData{
		Domains:  []string{"example.com"},
		Type:     TypeProxy,
		Servers:  []Server{{Address: "app:8080", MaxFails: 3, FailTimeout: "10s", Resolve: true}},
		BodySize: "10M",
		Balance:  BalanceRoundRobin,
	}

	confPath, err := GenerateAndSave(confDir, data)
	if err != nil {
		t.Fatalf("GenerateAndSave returned error: %v", err)
	}

	content, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	got := string(content)
	if !strings.Contains(got, "server app:8080 max_fails=3 fail_timeout=10s resolve;") {
		t.Fatalf("expected resolve directive for container names\n%s", got)
	}
	if strings.Contains(got, "least_conn;") {
		t.Fatalf("round robin must not emit a balancing directive\n%s", got)
	}
}

func TestGeneratedConfigSkipsResolveForStaticHosts(t *testing.T) {
	confDir := t.TempDir()

	data := ConfigData{
		Domains:  []string{"example.com"},
		Type:     TypeProxy,
		Servers:  []Server{{Address: "host.docker.internal:8080", MaxFails: 3, FailTimeout: "10s"}},
		BodySize: "10M",
	}

	confPath, err := GenerateAndSave(confDir, data)
	if err != nil {
		t.Fatalf("GenerateAndSave returned error: %v", err)
	}

	content, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	got := string(content)
	if strings.Contains(got, " resolve;") {
		t.Fatalf("host.docker.internal must not use the Docker resolver\n%s", got)
	}
	if !strings.Contains(got, "server host.docker.internal:8080 max_fails=3 fail_timeout=10s;") {
		t.Fatalf("expected static upstream server\n%s", got)
	}
}

func TestGeneratedConfigForStreamDisablesBuffering(t *testing.T) {
	confDir := t.TempDir()

	data := ConfigData{
		Domains:  []string{"s3.example.com"},
		Type:     TypeStream,
		Servers:  []Server{{Address: "host.docker.internal:18080", RelayPort: "18080", MaxFails: 3, FailTimeout: "10s"}},
		BodySize: "0",
	}

	confPath, err := GenerateAndSave(confDir, data)
	if err != nil {
		t.Fatalf("GenerateAndSave returned error: %v", err)
	}

	content, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	got := string(content)
	for _, want := range []string{
		"proxy_buffering         off;",
		"proxy_request_buffering off;",
		"client_max_body_size 0;",
		"proxy_read_timeout    3600s;",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated config missing %q\n%s", want, got)
		}
	}
	if ports := data.RelayPorts(); len(ports) != 1 || ports[0] != "18080" {
		t.Fatalf("relay ports = %v, want [18080]", ports)
	}
}

func TestBalanceDirectives(t *testing.T) {
	tests := map[string]string{
		BalanceRoundRobin: "",
		BalanceLeastConn:  "least_conn;",
		BalanceIPHash:     "ip_hash;",
		BalanceHashURI:    "hash $request_uri consistent;",
		BalanceRandomTwo:  "random two least_conn;",
	}

	for balance, want := range tests {
		got := (ConfigData{Balance: balance}).BalanceDirective()
		if got != want {
			t.Fatalf("balance %q rendered %q, want %q", balance, got, want)
		}
	}
}

func TestGeneratedConfigForRedirectHasNoUpstream(t *testing.T) {
	confDir := t.TempDir()

	data := ConfigData{
		Domains:    []string{"www.example.com"},
		Type:       TypeRedirect,
		RedirectTo: "https://example.com",
		BodySize:   "10M",
		SSL:        true,
	}

	confPath, err := GenerateAndSave(confDir, data)
	if err != nil {
		t.Fatalf("GenerateAndSave returned error: %v", err)
	}

	content, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}

	got := string(content)
	if strings.Contains(got, "upstream ") {
		t.Fatalf("redirect config must not contain an upstream\n%s", got)
	}
	if !strings.Contains(got, "return 301 https://example.com$request_uri;") {
		t.Fatalf("missing redirect target\n%s", got)
	}

	parsed, err := ParseConfig(confPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if parsed.Type != TypeRedirect || parsed.RedirectTo != "https://example.com" || !parsed.SSL {
		t.Fatalf("parsed redirect = %+v", parsed)
	}
}
