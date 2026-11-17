package nginx

import (
	_ "embed"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed template.tmpl
var configTemplate string

type ConfigData struct {
	Domains      string
	ServerNames  string
	Primary      string
	Type         string
	Upstream     string
	UpstreamName string
	RedirectTo   string
	BodySize     string
	SSL          bool
	Resolve      bool
	RelayPort    string
	ProxyHeaders string
	ExtraConfig  string
}

func GenerateAndSave(confDir string, data ConfigData) (string, error) {
	tmpl, err := template.New("nginx").Parse(configTemplate)
	if err != nil {
		return "", err
	}

	confPath := filepath.Join(confDir, data.Primary+".conf")
	tmpPath := confPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return "", err
	}

	if err := os.Rename(tmpPath, confPath); err != nil {
		return "", err
	}

	return confPath, nil
}
