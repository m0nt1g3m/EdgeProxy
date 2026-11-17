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

func proxyData() nginx.ConfigData {
	return nginx.ConfigData{
		Domains: []string{"example.com"},
		Type:    nginx.TypeProxy,
		Servers: []nginx.Server{{
			Address:     "app:8080",
			MaxFails:    nginx.DefaultMaxFails,
			FailTimeout: nginx.DefaultFailTimeout,
			Resolve:     true,
		}},
		BodySize: "10M",
	}
}

func TestCertExistsReportsFilesInRenewalDir(t *testing.T) {
	edgeDir := t.TempDir()
	renewalDir := filepath.Join(edgeDir, "certbot", "conf", "renewal")
	liveDir := filepath.Join(edgeDir, "certbot", "conf", "live", "example.com")
	for _, dir := range []string{renewalDir, liveDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	if err := os.WriteFile(filepath.Join(renewalDir, "example.com.conf"), []byte("renewal"), 0o644); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(liveDir, "fullchain.pem"), []byte("cert"), 0o644); err != nil {
		t.Fatalf("write fullchain file: %v", err)
	}

	manager := NewManager(edgeDir, "admin@example.com")
	if !manager.certExists("example.com") {
		t.Fatal("expected existing certificate to be reported")
	}
	if manager.certExists("missing.example.com") {
		t.Fatal("expected missing certificate to be reported as absent")
	}

	if err := os.WriteFile(filepath.Join(renewalDir, "empty.example.com.conf"), nil, 0o644); err != nil {
		t.Fatalf("write empty renewal file: %v", err)
	}
	if manager.certExists("empty.example.com") {
		t.Fatal("expected empty renewal file to be treated as missing")
	}
}

func TestIssueCertBuildsDryRunCommand(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := NewManager(dir, "admin@example.com")
	data := nginx.ConfigData{Domains: []string{"example.com", "www.example.com"}}

	if err := manager.issueCert(data, true, false); err != nil {
		t.Fatalf("issueCert returned error: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	want := "compose run --rm --entrypoint certbot certbot certonly --webroot -w /var/www/certbot --cert-name example.com --expand --email admin@example.com --agree-tos --no-eff-email --non-interactive --dry-run -d example.com -d www.example.com"
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
	data := proxyData()
	data.SSL = true

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

func TestDeployReportsNginxTestOutputAndRollsBack(t *testing.T) {
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

	err := manager.Deploy(proxyData())
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

func TestDeployRollsBackExistingConfigOnFailure(t *testing.T) {
	dir := t.TempDir()
	stubPath := filepath.Join(dir, "docker")
	script := `#!/bin/sh
case "$*" in
  *"exec -T nginx nginx -t"*) echo "nginx: [emerg] broken"; exit 1 ;;
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
	old := []byte("# previous config\n")
	if err := os.WriteFile(filepath.Join(confDir, "example.com.conf"), old, 0o644); err != nil {
		t.Fatalf("write old config: %v", err)
	}

	manager := NewManager(dir, "admin@example.com")
	if err := manager.Deploy(proxyData()); err == nil {
		t.Fatal("expected Deploy to fail")
	}

	restored, err := os.ReadFile(filepath.Join(confDir, "example.com.conf"))
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(restored) != string(old) {
		t.Fatalf("old config was not restored: %q", restored)
	}
}

func TestDeploySavesSiteSettings(t *testing.T) {
	dir, _ := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := NewManager(dir, "admin@example.com")
	if err := manager.Deploy(proxyData()); err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	site, err := manager.Store.Load("example.com")
	if err != nil {
		t.Fatalf("Load site: %v", err)
	}
	if !site.Managed {
		t.Fatal("expected site settings to be saved")
	}
	if len(site.Data.Servers) != 1 || site.Data.Servers[0].Address != "app:8080" {
		t.Fatalf("saved servers = %+v", site.Data.Servers)
	}
}

func TestIssueCertRemovesDummyLineageBeforeRealIssuance(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := NewManager(dir, "admin@example.com")
	data := nginx.ConfigData{Domains: []string{"example.com"}}

	if err := manager.issueCert(data, false, true); err != nil {
		t.Fatalf("issueCert returned error: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	content := string(got)
	removeIndex := strings.Index(content, "rm -rf /etc/letsencrypt/live/example.com")
	issueIndex := strings.Index(content, "compose run --rm --entrypoint certbot certbot certonly")
	if removeIndex == -1 || issueIndex == -1 {
		t.Fatalf("expected dummy removal and certbot run, got: %q", content)
	}
	if removeIndex > issueIndex {
		t.Fatalf("expected dummy lineage removal before certbot, got: %q", content)
	}
	if strings.Contains(content, "--dry-run") {
		t.Fatalf("real issuance must not use --dry-run: %q", content)
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

func TestPruneUnusedRelays(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	stubPath := filepath.Join(dir, "docker")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + logPath + `"
case "$*" in
  *"ps -a --filter name=edgeproxy-hostrelay-"*)
    printf 'edgeproxy-hostrelay-8080\nedgeproxy-hostrelay-9090\n'
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
	conf := "upstream up_example {\n    server host.docker.internal:8080;\n}\n"
	if err := os.WriteFile(filepath.Join(confDir, "example.com.conf"), []byte(conf), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := NewManager(dir, "admin@example.com")
	manager.pruneUnusedRelays()

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	content := string(got)
	if strings.Contains(content, "rm -f edgeproxy-hostrelay-8080") {
		t.Fatalf("referenced relay must be kept: %s", content)
	}
	if !strings.Contains(content, "rm -f edgeproxy-hostrelay-9090") {
		t.Fatalf("unreferenced relay must be removed: %s", content)
	}
}

func TestDeleteRemovesConfigAndSettings(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := NewManager(dir, "admin@example.com")
	if err := manager.Deploy(proxyData()); err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	if err := manager.Delete("example.com"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	confPath := filepath.Join(dir, "nginx", "conf.d", "example.com.conf")
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Fatalf("expected config to be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "sites", "example.com.json")); !os.IsNotExist(err) {
		t.Fatalf("expected site settings to be removed")
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(got), "compose exec -T nginx nginx -s reload") {
		t.Fatalf("expected nginx reload: %s", got)
	}
}
