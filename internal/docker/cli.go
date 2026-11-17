package docker

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Run executes a docker CLI command and returns its combined output.
func Run(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	return out.String(), err
}

// RunError is returned when a docker command fails, preserving its output so
// callers can show the real reason instead of only "exit status 1".
func RunError(err error, output string) error {
	output = strings.TrimSpace(output)
	if output == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, output)
}
