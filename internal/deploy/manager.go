package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"EdgeProxy/internal/docker"
	"EdgeProxy/internal/nginx"
)

type Manager struct {
	EdgeDir string
	Compose *docker.Compose
	Email   string
}

func NewManager(edgeDir, email string) *Manager {
	return &Manager{
		EdgeDir: edgeDir,
		Compose: docker.NewCompose(edgeDir),
		Email:   email,
	}
}

func (m *Manager) Deploy(data nginx.ConfigData) error {
	confDir := filepath.Join(m.EdgeDir, "nginx", "conf.d")

	confPath, err := nginx.GenerateAndSave(confDir, data)
	if err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}

	if _, err := m.Compose.Run("exec", "-T", "nginx", "nginx", "-t"); err != nil {
		os.Remove(confPath)
		return fmt.Errorf("nginx config test failed, rolled back: %w", err)
	}

	isNewCert := data.SSL && !m.certExists(data.Primary)
	if isNewCert {
		if err := m.createDummyCert(data.Primary); err != nil {
			os.Remove(confPath)
			return err
		}
	}

	if _, err := m.Compose.Run("exec", "-T", "nginx", "nginx", "-s", "reload"); err != nil {
		os.Remove(confPath)
		return fmt.Errorf("nginx reload failed: %w", err)
	}

	if isNewCert {
		if err := m.issueCert(data, true); err != nil {
			return fmt.Errorf("certbot dry-run failed (port 80 unreachable?): %w", err)
		}

		if err := m.issueCert(data, false); err != nil {
			return fmt.Errorf("certbot failed, nginx kept dummy cert: %w", err)
		}

		m.Compose.Run("exec", "-T", "nginx", "nginx", "-s", "reload")
	}

	return nil
}

func (m *Manager) certExists(domain string) bool {
	path := filepath.Join(m.EdgeDir, "certbot", "conf", "renewal", domain+".conf")
	_, err := os.Stat(path)
	return err == nil
}

func (m *Manager) createDummyCert(domain string) error {
	script := fmt.Sprintf(`mkdir -p /etc/letsencrypt/live/%s && \
	openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
	-keyout /etc/letsencrypt/live/%s/privkey.pem \
	-out    /etc/letsencrypt/live/%s/fullchain.pem \
	-subj '/CN=%s' 2>/dev/null`, domain, domain, domain, domain)

	_, err := m.Compose.Run("run", "--rm", "--entrypoint", "sh", "certbot", "-c", script)
	return err
}

func (m *Manager) issueCert(data nginx.ConfigData, dryRun bool) error {
	args := []string{"run", "--rm", "--entrypoint", "certbot", "certbot", "certonly",
		"--webroot", "-w", "/var/www/certbot",
		"--email", m.Email, "--agree-tos", "--no-eff-email", "--non-interactive"}

	if dryRun {
		args = append(args, "--dry-run")
	}

	for _, d := range strings.Split(data.Domains, ",") {
		args = append(args, "-d", strings.TrimSpace(d))
	}

	_, err := m.Compose.Run(args...)
	return err
}
