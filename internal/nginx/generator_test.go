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
