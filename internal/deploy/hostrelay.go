package deploy

import (
	"fmt"
	"strconv"
	"strings"

	"EdgeProxy/internal/docker"
)

const (
	relayImage  = "alpine/socat:latest"
	relayPrefix = "edgeproxy-hostrelay-"
)

// ensureHostRelay makes sure a host service listening on 127.0.0.1:<port> is
// reachable from the nginx container as host.docker.internal:<port>.
//
// A small socat container runs in the host network namespace and listens on
// the Docker host gateway address. This keeps the upstream service bound to
// loopback while still allowing the proxy container to reach it.
func (m *Manager) ensureHostRelay(port string) error {
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("invalid relay port %q", port)
	}

	name := relayPrefix + port

	if out, err := docker.Run("inspect", "--format", "{{.State.Running}}", name); err == nil {
		if strings.TrimSpace(out) == "true" {
			return nil
		}

		if out, err := docker.Run("start", name); err != nil {
			return docker.RunError(err, out)
		}
		return nil
	}

	script := fmt.Sprintf(
		`B=$(getent hosts host.docker.internal | awk '{print $1}'); `+
			`[ -n "$B" ] || { echo "host.docker.internal did not resolve" >&2; exit 1; }; `+
			`exec socat TCP-LISTEN:%d,bind=$B,reuseaddr,fork TCP:127.0.0.1:%d`,
		portNumber, portNumber,
	)

	out, err := docker.Run(
		"run", "-d",
		"--name", name,
		"--restart", "unless-stopped",
		"--network", "host",
		"--add-host", "host.docker.internal:host-gateway",
		"--entrypoint", "sh",
		relayImage,
		"-c", script,
	)
	if err != nil {
		return docker.RunError(err, out)
	}

	return nil
}
