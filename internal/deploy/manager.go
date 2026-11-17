package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"EdgeProxy/internal/docker"
	"EdgeProxy/internal/nginx"
	"EdgeProxy/internal/state"
)

type Manager struct {
	EdgeDir string
	Compose *docker.Compose
	Email   string
	Store   *state.Store
}

func NewManager(edgeDir, email string) *Manager {
	return &Manager{
		EdgeDir: edgeDir,
		Compose: docker.NewCompose(edgeDir),
		Email:   email,
		Store:   state.New(edgeDir),
	}
}

// Deploy creates or updates a site.
func (m *Manager) Deploy(data nginx.ConfigData) error {
	return m.apply(nil, data)
}

// DeployReplacing updates an existing site. If the primary domain changed,
// the old config and settings are removed after the new one is live.
func (m *Manager) DeployReplacing(previous nginx.ConfigData, data nginx.ConfigData) error {
	return m.apply(&previous, data)
}

func (m *Manager) apply(previous *nginx.ConfigData, data nginx.ConfigData) error {
	confDir := m.Store.ConfDir()
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return fmt.Errorf("create nginx config directory: %w", err)
	}

	targetPath := m.Store.ConfPath(data.Primary())
	previousTarget, targetExisted := readFileIfExists(targetPath)

	primaryChanged := previous != nil && previous.Primary() != "" && previous.Primary() != data.Primary()
	var previousOldPath string
	var previousOldContent []byte
	if primaryChanged {
		previousOldPath = m.Store.ConfPath(previous.Primary())
		previousOldContent, _ = readFileIfExists(previousOldPath)
	}

	rollback := func() {
		if targetExisted {
			_ = os.WriteFile(targetPath, previousTarget, 0o644)
		} else {
			_ = os.Remove(targetPath)
		}
		if primaryChanged && previousOldContent != nil {
			_ = os.WriteFile(previousOldPath, previousOldContent, 0o644)
		}
	}

	isNewCert := data.SSL && !m.certExists(data.Primary())
	domainsChanged := previous != nil && !sameDomains(previous.Domains, data.Domains)
	needsCert := data.SSL && (isNewCert || domainsChanged)
	if needsCert && m.Email == "" {
		return fmt.Errorf("EMAIL is required to issue certificates")
	}

	if _, err := nginx.GenerateAndSave(confDir, data); err != nil {
		rollback()
		return fmt.Errorf("failed to generate config: %w", err)
	}

	if isNewCert {
		if err := m.createDummyCert(data.Primary()); err != nil {
			rollback()
			return fmt.Errorf("failed to create temporary certificate: %w", err)
		}
	}

	for _, port := range data.RelayPorts() {
		if err := m.ensureHostRelay(port); err != nil {
			rollback()
			return fmt.Errorf("failed to expose 127.0.0.1:%s to the proxy: %w", port, err)
		}
	}

	if out, err := m.Compose.Run("exec", "-T", "nginx", "nginx", "-t"); err != nil {
		rollback()
		return fmt.Errorf("nginx config test failed, rolled back: %w\n%s", err, strings.TrimSpace(out))
	}

	if primaryChanged {
		if err := os.Remove(previousOldPath); err != nil && !os.IsNotExist(err) {
			rollback()
			return fmt.Errorf("remove previous config: %w", err)
		}
		if out, err := m.Compose.Run("exec", "-T", "nginx", "nginx", "-t"); err != nil {
			rollback()
			return fmt.Errorf("nginx config test failed after removing the old domain, rolled back: %w\n%s", err, strings.TrimSpace(out))
		}
	}

	if out, err := m.Compose.Run("exec", "-T", "nginx", "nginx", "-s", "reload"); err != nil {
		rollback()
		_, _ = m.Compose.Run("exec", "-T", "nginx", "nginx", "-s", "reload")
		return fmt.Errorf("nginx reload failed, rolled back: %w\n%s", err, strings.TrimSpace(out))
	}
	defer m.pruneUnusedRelays()

	if err := m.Store.Save(data); err != nil {
		return fmt.Errorf("config is live, but saving site settings failed: %w", err)
	}
	if primaryChanged {
		if err := m.Store.Delete(previous.Primary()); err != nil {
			return fmt.Errorf("config is live, but removing the old site settings failed: %w", err)
		}
	}

	if needsCert {
		if err := m.issueCert(data, true, isNewCert); err != nil {
			return fmt.Errorf("certbot dry-run failed (port 80 unreachable?): %w", err)
		}

		if err := m.issueCert(data, false, isNewCert); err != nil {
			return fmt.Errorf("certbot failed, nginx kept its previous certificate: %w", err)
		}

		if out, err := m.Compose.Run("exec", "-T", "nginx", "nginx", "-s", "reload"); err != nil {
			return fmt.Errorf("certificate installed, but nginx reload failed: %w\n%s", err, strings.TrimSpace(out))
		}
	}

	return nil
}

// Delete removes a site config and its saved settings.
func (m *Manager) Delete(domain string) error {
	_, loadErr := m.Store.Load(domain)
	confPath := m.Store.ConfPath(domain)
	previous, existed := readFileIfExists(confPath)

	if !existed {
		if loadErr != nil {
			return fmt.Errorf("%w: %s", state.ErrNotFound, domain)
		}
		return m.Store.Delete(domain)
	}

	if err := os.Remove(confPath); err != nil {
		return fmt.Errorf("remove config: %w", err)
	}

	if out, err := m.Compose.Run("exec", "-T", "nginx", "nginx", "-t"); err != nil {
		_ = os.WriteFile(confPath, previous, 0o644)
		return fmt.Errorf("nginx config test failed, rolled back: %w\n%s", err, strings.TrimSpace(out))
	}

	if out, err := m.Compose.Run("exec", "-T", "nginx", "nginx", "-s", "reload"); err != nil {
		_ = os.WriteFile(confPath, previous, 0o644)
		_, _ = m.Compose.Run("exec", "-T", "nginx", "nginx", "-s", "reload")
		return fmt.Errorf("nginx reload failed, rolled back: %w\n%s", err, strings.TrimSpace(out))
	}

	if err := m.Store.Delete(domain); err != nil {
		return fmt.Errorf("config removed, but deleting site settings failed: %w", err)
	}

	m.pruneUnusedRelays()

	return nil
}

func (m *Manager) certExists(domain string) bool {
	renewalPath := filepath.Join(m.EdgeDir, "certbot", "conf", "renewal", domain+".conf")
	info, err := os.Stat(renewalPath)
	if err != nil || info.Size() == 0 {
		return false
	}

	fullchainPath := filepath.Join(m.EdgeDir, "certbot", "conf", "live", domain, "fullchain.pem")
	info, err = os.Stat(fullchainPath)
	return err == nil && info.Size() > 0
}

func (m *Manager) createDummyCert(domain string) error {
	script := fmt.Sprintf(`mkdir -p /etc/letsencrypt/live/%s && \
	openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
	-keyout /etc/letsencrypt/live/%s/privkey.pem \
	-out    /etc/letsencrypt/live/%s/fullchain.pem \
	-subj '/CN=%s' 2>/dev/null`, domain, domain, domain, domain)

	out, err := m.Compose.Run("run", "--rm", "--entrypoint", "sh", "certbot", "-c", script)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(out))
	}
	return nil
}

func (m *Manager) issueCert(data nginx.ConfigData, dryRun, removeDummy bool) error {
	if m.Email == "" {
		return fmt.Errorf("EMAIL is required to issue certificates")
	}

	if !dryRun && removeDummy {
		if err := m.removeDummyCert(data.Primary()); err != nil {
			return fmt.Errorf("failed to remove temporary certificate: %w", err)
		}
	}

	args := []string{"run", "--rm", "--entrypoint", "certbot", "certbot", "certonly",
		"--webroot", "-w", "/var/www/certbot",
		"--cert-name", data.Primary(),
		"--expand",
		"--email", m.Email, "--agree-tos", "--no-eff-email", "--non-interactive"}

	if dryRun {
		args = append(args, "--dry-run")
	}

	for _, domain := range data.Domains {
		args = append(args, "-d", domain)
	}

	out, err := m.Compose.Run(args...)
	if err != nil {
		if !dryRun && removeDummy {
			// Keep nginx reloadable if issuance fails after the dummy was removed.
			_ = m.createDummyCert(data.Primary())
		}
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(out))
	}
	return nil
}

func (m *Manager) removeDummyCert(domain string) error {
	script := fmt.Sprintf(
		`rm -rf /etc/letsencrypt/live/%s /etc/letsencrypt/archive/%s /etc/letsencrypt/renewal/%s.conf`,
		domain, domain, domain,
	)

	out, err := m.Compose.Run("run", "--rm", "--entrypoint", "sh", "certbot", "-c", script)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(out))
	}
	return nil
}

// pruneUnusedRelays removes socat containers whose port is no longer
// referenced by any generated nginx config. Ports that still appear in a
// config are kept.
func (m *Manager) pruneUnusedRelays() {
	out, err := docker.Run("ps", "-a", "--filter", "name="+relayPrefix, "--format", "{{.Names}}")
	if err != nil {
		return
	}

	for _, name := range strings.Fields(out) {
		if !strings.HasPrefix(name, relayPrefix) {
			continue
		}
		port := strings.TrimPrefix(name, relayPrefix)
		if port == "" || m.relayPortReferenced(port) {
			continue
		}
		_, _ = docker.Run("rm", "-f", name)
	}
}

func (m *Manager) relayPortReferenced(port string) bool {
	entries, err := os.ReadDir(m.Store.ConfDir())
	if err != nil {
		return true // be conservative
	}

	pattern := regexp.MustCompile(regexp.QuoteMeta("host.docker.internal:"+port) + `([^0-9]|$)`)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(m.Store.ConfDir(), entry.Name()))
		if err != nil {
			return true
		}
		if pattern.Match(content) {
			return true
		}
	}
	return false
}

func readFileIfExists(path string) ([]byte, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return content, true
}

func sameDomains(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	normalize := func(domains []string) []string {
		result := make([]string, 0, len(domains))
		for _, domain := range domains {
			result = append(result, strings.ToLower(strings.TrimSpace(domain)))
		}
		sort.Strings(result)
		return result
	}

	left = normalize(left)
	right = normalize(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
