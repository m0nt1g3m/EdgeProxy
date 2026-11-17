package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"EdgeProxy/internal/nginx"
)

func writeDockerStub(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	scriptPath := filepath.Join(dir, "docker")

	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	return dir, logPath
}

func TestCertExistsReportsFilesInRenewalDir(t *testing.T) {
	edgeDir := t.TempDir()
	renewalDir := filepath.Join(edgeDir, "certbot", "conf", "renewal")
	if err := os.MkdirAll(renewalDir, 0o755); err != nil {
		t.Fatalf("mkdir renewal dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(renewalDir, "example.com.conf"), []byte("renewal"), 0o644); err != nil {
		t.Fatalf("write cert file: %v", err)
	}

	manager := NewManager(edgeDir, "admin@example.com")
	if !manager.certExists("example.com") {
		t.Fatal("expected existing certificate to be reported")
	}
	if manager.certExists("missing.example.com") {
		t.Fatal("expected missing certificate to be reported as absent")
	}
}

func TestIssueCertBuildsDryRunCommand(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := NewManager(dir, "admin@example.com")
	data := nginx.ConfigData{Domains: "example.com, www.example.com"}

	if err := manager.issueCert(data, true); err != nil {
		t.Fatalf("issueCert returned error: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	want := "compose run --rm --entrypoint certbot certbot certonly --webroot -w /var/www/certbot --email admin@example.com --agree-tos --no-eff-email --non-interactive --dry-run -d example.com -d www.example.com"
	if gotStr := strings.TrimSpace(string(got)); gotStr != want {
		t.Fatalf("unexpected certbot args: %q\nwant: %q", gotStr, want)
	}
}

func TestDeployCreatesDummyCertBeforeNginxValidation(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	confDir := filepath.Join(dir, "nginx", "conf.d")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("mkdir conf dir: %v", err)
	}

	manager := NewManager(dir, "admin@example.com")
	data := nginx.ConfigData{
		Domains:      "example.com",
		ServerNames:  "example.com",
		Primary:      "example.com",
		Type:         "proxy",
		Upstream:     "app:8080",
		UpstreamName: "up_example_com",
		BodySize:     "10M",
		SSL:          true,
	}

	if err := manager.Deploy(data); err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	content := string(got)
	certIndex := strings.Index(content, "compose run --rm --entrypoint sh certbot -c")
	nginxIndex := strings.Index(content, "compose exec -T nginx nginx -t")
	if certIndex == -1 || nginxIndex == -1 {
		t.Fatalf("expected both cert creation and nginx validation calls, got: %q", content)
	}
	if certIndex > nginxIndex {
		t.Fatalf("expected dummy cert creation before nginx validation, got: %q", content)
	}
}

func TestDeployReportsNginxTestOutput(t *testing.T) {
	dir := t.TempDir()
	stubPath := filepath.Join(dir, "docker")
	script := `#!/bin/sh
case "$*" in
  *"exec -T nginx nginx -t"*)
    echo "nginx: [emerg] duplicate listen options for 0.0.0.0:80"
    exit 1
    ;;
esac
exit 0
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	confDir := filepath.Join(dir, "nginx", "conf.d")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("mkdir conf dir: %v", err)
	}

	manager := NewManager(dir, "admin@example.com")
	data := nginx.ConfigData{
		Domains:      "example.com",
		ServerNames:  "example.com",
		Primary:      "example.com",
		Type:         "proxy",
		Upstream:     "app:8080",
		UpstreamName: "up_example_com",
		BodySize:     "10M",
	}

	err := manager.Deploy(data)
	if err == nil {
		t.Fatal("expected Deploy to fail")
	}
	if !strings.Contains(err.Error(), "duplicate listen options") {
		t.Fatalf("expected nginx output in error, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(confDir, "example.com.conf")); !os.IsNotExist(statErr) {
		t.Fatalf("expected failed config to be rolled back")
	}
}

func TestEnsureHostRelayStartsSocatOnHostGateway(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Simulate a missing relay container so the run command is exercised.
	stubPath := filepath.Join(dir, "docker")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + logPath + `"
case "$*" in
  "inspect "*) exit 1 ;;
esac
exit 0
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	manager := NewManager(dir, "admin@example.com")
	if err := manager.ensureHostRelay("8080"); err != nil {
		t.Fatalf("ensureHostRelay returned error: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	content := string(got)
	for _, want := range []string{
		"run -d --name edgeproxy-hostrelay-8080",
		"--network host",
		"alpine/socat:latest",
		"TCP-LISTEN:8080,bind=$B,reuseaddr,fork TCP:127.0.0.1:8080",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("relay command missing %q\n%s", want, content)
		}
	}
}
