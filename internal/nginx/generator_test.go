package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndSaveCreatesConfigFile(t *testing.T) {
	confDir := t.TempDir()

	data := ConfigData{
		Domains:      "example.com, www.example.com",
		ServerNames:  "example.com www.example.com",
		Primary:      "example.com",
		Type:         "proxy",
		Upstream:     "app:8080",
		UpstreamName: "app",
		BodySize:     "100m",
		SSL:          true,
		ProxyHeaders: "\n        proxy_set_header X-Forwarded-Proto $scheme;",
		ExtraConfig:  "\n        proxy_set_header Host $host;",
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
		"upstream app {",
		"proxy_pass http://app;",
		"ssl_certificate     /etc/letsencrypt/live/example.com/fullchain.pem;",
		"ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated config missing %q\n%s", want, got)
		}
	}
}

func TestGenerateAndSaveFailsForMissingDirectory(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing", "conf")

	_, err := GenerateAndSave(missingDir, ConfigData{Primary: "example.com"})
	if err == nil {
		t.Fatal("expected error when target directory does not exist")
	}
}

func TestGeneratedConfigSupportsMultipleDomains(t *testing.T) {
	confDir := t.TempDir()

	data := ConfigData{
		Domains:      "example.com",
		ServerNames:  "example.com",
		Primary:      "example.com",
		Type:         "proxy",
		Upstream:     "app:8080",
		UpstreamName: "up_example_com",
		BodySize:     "10M",
		Resolve:      true,
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
	if strings.Contains(got, "reuseport") {
		t.Fatalf("reuseport must not be used: nginx rejects it in more than one server block\n%s", got)
	}
	if !strings.Contains(got, "server app:8080 resolve;") {
		t.Fatalf("expected resolve directive for container names\n%s", got)
	}
}

func TestGeneratedConfigSkipsResolveForStaticHosts(t *testing.T) {
	confDir := t.TempDir()

	data := ConfigData{
		Domains:      "example.com",
		ServerNames:  "example.com",
		Primary:      "example.com",
		Type:         "proxy",
		Upstream:     "host.docker.internal:8080",
		UpstreamName: "up_example_com",
		BodySize:     "10M",
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
	if strings.Contains(got, "resolve;") {
		t.Fatalf("host.docker.internal must not use the Docker resolver\n%s", got)
	}
	if !strings.Contains(got, "server host.docker.internal:8080;") {
		t.Fatalf("expected static upstream server\n%s", got)
	}
}
