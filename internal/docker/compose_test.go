package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestComposeRunUsesDockerComposeCommand(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	compose := NewCompose(dir)
	if _, err := compose.Run("ps"); err != nil {
		t.Fatalf("compose.Run returned error: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if gotStr := strings.TrimSpace(string(got)); gotStr != "compose ps" {
		t.Fatalf("unexpected docker args: %q", gotStr)
	}
}

func TestComposeExecFormatsExecCommand(t *testing.T) {
	dir, logPath := writeDockerStub(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	compose := NewCompose(dir)
	if err := compose.Exec("web", "echo", "hello"); err != nil {
		t.Fatalf("compose.Exec returned error: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if gotStr := strings.TrimSpace(string(got)); gotStr != "compose exec -T web echo hello" {
		t.Fatalf("unexpected docker exec args: %q", gotStr)
	}
}
