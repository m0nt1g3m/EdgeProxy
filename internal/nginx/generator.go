package nginx

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed template.tmpl
var configTemplate string

// GenerateAndSave renders the site and atomically writes it to confDir.
func GenerateAndSave(confDir string, data ConfigData) (string, error) {
	if data.Primary() == "" {
		return "", fmt.Errorf("at least one domain is required")
	}

	data.ApplyDefaults()

	tmpl, err := template.New("nginx").Parse(configTemplate)
	if err != nil {
		return "", err
	}

	confPath := filepath.Join(confDir, data.Primary()+".conf")
	tmpPath := confPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpPath)

	if err := tmpl.Execute(f, data); err != nil {
		f.Close()
		return "", err
	}

	if err := f.Close(); err != nil {
		return "", err
	}

	if err := os.Rename(tmpPath, confPath); err != nil {
		return "", err
	}

	return confPath, nil
}
