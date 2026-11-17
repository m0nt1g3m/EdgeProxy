package cli

import (
	"strings"
	"testing"
)

func TestBuildConfigNormalizesLoopbackUpstream(t *testing.T) {
	data, err := BuildConfig("proxy.dondegusto.ru", "proxy", "127.0.0.1:8080", "", "10M", true)
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	if data.Upstream != "host.docker.internal:8080" {
		t.Fatalf("upstream = %q, want host.docker.internal:8080", data.Upstream)
	}
	if data.RelayPort != "8080" {
		t.Fatalf("relay port = %q, want 8080", data.RelayPort)
	}
	if data.Resolve {
		t.Fatal("host.docker.internal must not use Docker DNS resolution")
	}
	if data.UpstreamName != "up_proxy_dondegusto_ru" {
		t.Fatalf("unexpected upstream name: %q", data.UpstreamName)
	}
}

func TestBuildConfigStreamDisablesBuffering(t *testing.T) {
	data, err := BuildConfig("s3.dondegusto.ru", "stream", "127.0.0.1:18080", "", "10M", true)
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	if data.BodySize != "0" {
		t.Fatalf("body size = %q, want 0", data.BodySize)
	}
	if !strings.Contains(data.ExtraConfig, "proxy_buffering") {
		t.Fatalf("expected buffering directive, got %q", data.ExtraConfig)
	}
}

func TestBuildConfigRequiresUpstream(t *testing.T) {
	if _, err := BuildConfig("example.com", "proxy", "", "", "10M", true); err == nil {
		t.Fatal("expected error for missing upstream")
	}
}

func TestBuildConfigRequiresRedirectTarget(t *testing.T) {
	if _, err := BuildConfig("www.example.com", "redirect", "", "", "10M", true); err == nil {
		t.Fatal("expected error for missing redirect target")
	}
}

func TestBuildConfigRejectsUnknownType(t *testing.T) {
	if _, err := BuildConfig("example.com", "unknown", "app:8080", "", "10M", true); err == nil {
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
		if _, err := BuildConfig(domain, "proxy", "app:8080", "", "10M", true); err == nil {
			t.Fatalf("expected error for domain %q", domain)
		}
	}
}

func TestBuildConfigRejectsRelativeRedirectTarget(t *testing.T) {
	if _, err := BuildConfig("www.example.com", "redirect", "", "/login", "10M", true); err == nil {
		t.Fatal("expected error for relative redirect target")
	}
}
