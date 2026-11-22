package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"EdgeProxy/internal/nginx"
)

// ErrNotFound is returned when a domain has neither a saved record nor a
// generated nginx config.
var ErrNotFound = errors.New("site not found")

// Site is a known site and where its settings came from.
type Site struct {
	Data    nginx.ConfigData
	Managed bool  // true when a saved settings file exists
	Err     error // set when an existing config could not be parsed
}

// Store persists site settings next to the generated nginx configs. Settings
// live in <edgeDir>/sites/<domain>.json and can be edited without touching
// nginx syntax by hand.
type Store struct {
	EdgeDir string
}

func New(edgeDir string) *Store {
	return &Store{EdgeDir: edgeDir}
}

func (s *Store) SitesDir() string {
	return filepath.Join(s.EdgeDir, "sites")
}

func (s *Store) ConfDir() string {
	return filepath.Join(s.EdgeDir, "nginx", "conf.d")
}

func (s *Store) ConfPath(domain string) string {
	return filepath.Join(s.ConfDir(), domain+".conf")
}

func (s *Store) recordPath(domain string) string {
	return filepath.Join(s.SitesDir(), domain+".json")
}

// Save writes the site record atomically.
func (s *Store) Save(data nginx.ConfigData) error {
	domain := data.Primary()
	if domain == "" {
		return fmt.Errorf("cannot save a site without a domain")
	}
	if err := validateName(domain); err != nil {
		return err
	}

	data.ApplyDefaults()

	if err := os.MkdirAll(s.SitesDir(), 0o755); err != nil {
		return fmt.Errorf("create sites directory: %w", err)
	}

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode site settings: %w", err)
	}
	encoded = append(encoded, '\n')

	path := s.recordPath(domain)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o644); err != nil {
		return fmt.Errorf("write site settings: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("save site settings: %w", err)
	}
	return nil
}

// Load returns saved settings, falling back to importing the generated nginx
// config when no settings file exists yet.
func (s *Store) Load(domain string) (Site, error) {
	if err := validateName(domain); err != nil {
		return Site{}, err
	}

	path := s.recordPath(domain)
	if encoded, err := os.ReadFile(path); err == nil {
		var data nginx.ConfigData
		if err := json.Unmarshal(encoded, &data); err != nil {
			return Site{}, fmt.Errorf("parse site settings %s: %w", path, err)
		}
		if len(data.Domains) == 0 {
			data.Domains = []string{domain}
		}
		data.ApplyDefaults()
		return Site{Data: data, Managed: true}, nil
	} else if !os.IsNotExist(err) {
		return Site{}, fmt.Errorf("read site settings: %w", err)
	}

	confPath := s.ConfPath(domain)
	data, err := nginx.ParseConfig(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Site{}, fmt.Errorf("%w: %s", ErrNotFound, domain)
		}
		return Site{}, fmt.Errorf("import existing config %s: %w", confPath, err)
	}

	// Keep the existing file name stable even if server_name lists a different
	// domain first.
	if data.Primary() != domain {
		data.Domains = nginx.EnsurePrimary(data.Domains, domain)
	}

	return Site{Data: data, Managed: false}, nil
}

// List returns every known site, combining saved records with configs that
// are still waiting to be imported.
func (s *Store) List() ([]Site, error) {
	seen := make(map[string]bool)
	var sites []Site

	entries, err := os.ReadDir(s.SitesDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read sites directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		domain := strings.TrimSuffix(entry.Name(), ".json")
		site, err := s.Load(domain)
		if err != nil {
			sites = append(sites, Site{Data: nginx.ConfigData{Domains: []string{domain}}, Err: err})
			seen[domain] = true
			continue
		}
		sites = append(sites, site)
		seen[domain] = true
	}

	confEntries, err := os.ReadDir(s.ConfDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read nginx config directory: %w", err)
	}
	for _, entry := range confEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		domain := strings.TrimSuffix(entry.Name(), ".conf")
		if seen[domain] {
			continue
		}
		data, parseErr := nginx.ParseConfig(filepath.Join(s.ConfDir(), entry.Name()))
		site := Site{Data: data, Managed: false}
		if parseErr != nil {
			site.Data = nginx.ConfigData{Domains: []string{domain}}
			site.Err = parseErr
		}
		sites = append(sites, site)
		seen[domain] = true
	}

	sort.Slice(sites, func(i, j int) bool {
		return sites[i].Data.Primary() < sites[j].Data.Primary()
	})

	return sites, nil
}

// Delete removes the saved settings for a domain. The nginx config itself is
// removed by the deploy manager, which can also test and reload nginx.
func (s *Store) Delete(domain string) error {
	if err := validateName(domain); err != nil {
		return err
	}

	err := os.Remove(s.recordPath(domain))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete site settings: %w", err)
	}
	return nil
}

func validateName(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	if strings.ContainsAny(domain, `/\`) || strings.Contains(domain, "..") {
		return fmt.Errorf("invalid domain %q", domain)
	}
	return nil
}
