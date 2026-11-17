package state

import (
	"os"
	"path/filepath"
	"testing"

	"EdgeProxy/internal/nginx"
)

func TestStoreSaveLoadListDelete(t *testing.T) {
	store := New(t.TempDir())

	data := nginx.ConfigData{
		Domains: []string{"example.com", "www.example.com"},
		Type:    nginx.TypeProxy,
		Servers: []nginx.Server{{
			Address:     "app:8080",
			MaxFails:    3,
			FailTimeout: "10s",
			Resolve:     true,
		}},
		BodySize: "10M",
		SSL:      true,
	}
	if err := store.Save(data); err != nil {
		t.Fatalf("Save: %v", err)
	}

	site, err := store.Load("example.com")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !site.Managed {
		t.Fatal("expected managed site")
	}
	if len(site.Data.Domains) != 2 || site.Data.Servers[0].Address != "app:8080" {
		t.Fatalf("loaded site = %+v", site.Data)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Data.Primary() != "example.com" {
		t.Fatalf("list = %+v", list)
	}

	if err := store.Delete("example.com"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Load("example.com"); err == nil {
		t.Fatal("expected ErrNotFound after delete")
	}
}

func TestStoreImportsExistingConfig(t *testing.T) {
	edgeDir := t.TempDir()
	store := New(edgeDir)

	confDir := store.ConfDir()
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	conf := `upstream up_legacy {
    zone up_legacy 64k;
    least_conn;
    server host.docker.internal:8080;
    keepalive 64;
}
server {
    listen 80;
    server_name legacy.example.com;
    location / { proxy_pass http://up_legacy; }
}
`
	if err := os.WriteFile(filepath.Join(confDir, "legacy.example.com.conf"), []byte(conf), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	site, err := store.Load("legacy.example.com")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if site.Managed {
		t.Fatal("expected imported site to be unmanaged")
	}
	if site.Data.Type != nginx.TypeProxy || len(site.Data.Servers) != 1 {
		t.Fatalf("imported site = %+v", site.Data)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Managed {
		t.Fatalf("list = %+v", list)
	}
}

func TestStoreImportKeepsFileNameAsPrimary(t *testing.T) {
	edgeDir := t.TempDir()
	store := New(edgeDir)

	confDir := store.ConfDir()
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	conf := `upstream up_legacy {
    server host.docker.internal:8080;
}
server {
    listen 80;
    server_name www.legacy.example.com legacy.example.com;
    location / { proxy_pass http://up_legacy; }
}
`
	if err := os.WriteFile(filepath.Join(confDir, "legacy.example.com.conf"), []byte(conf), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	site, err := store.Load("legacy.example.com")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if site.Data.Primary() != "legacy.example.com" {
		t.Fatalf("primary = %q, want legacy.example.com", site.Data.Primary())
	}
	if len(site.Data.Domains) != 2 || site.Data.Domains[1] != "www.legacy.example.com" {
		t.Fatalf("domains = %v", site.Data.Domains)
	}
}

func TestStoreRejectsPathTraversal(t *testing.T) {
	store := New(t.TempDir())

	for _, name := range []string{"", "../evil", "a/b", `a\b`} {
		if _, err := store.Load(name); err == nil {
			t.Fatalf("expected error for %q", name)
		}
		if err := store.Delete(name); err == nil {
			t.Fatalf("expected delete error for %q", name)
		}
	}
}
