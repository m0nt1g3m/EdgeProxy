package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// OpenEditor opens path in the user's editor. VISUAL takes precedence over
// EDITOR, and vi is used when neither is set.
func OpenEditor(path string) error {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		editor = "vi"
	}

	// Pass the path as a positional argument so editors with their own flags
	// work and paths containing spaces stay intact.
	cmd := exec.Command("sh", "-c", `exec `+editor+` "$1"`, "sh", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", editor, err)
	}
	return nil
}
