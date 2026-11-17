package nginx

import "testing"

func TestParseServers(t *testing.T) {
	servers, err := ParseServers("app1:8080, app2:8080 weight=2 max_fails=5 fail_timeout=30s, 127.0.0.1:9000 backup")
	if err != nil {
		t.Fatalf("ParseServers returned error: %v", err)
	}

	if len(servers) != 3 {
		t.Fatalf("got %d servers, want 3", len(servers))
	}

	first := servers[0]
	if first.Address != "app1:8080" || !first.Resolve {
		t.Fatalf("first server = %+v", first)
	}
	if first.MaxFails != DefaultMaxFails || first.FailTimeout != DefaultFailTimeout {
		t.Fatalf("first defaults = %+v", first)
	}

	second := servers[1]
	if second.Weight != 2 || second.MaxFails != 5 || second.FailTimeout != "30s" {
		t.Fatalf("second server = %+v", second)
	}

	third := servers[2]
	if third.Address != "host.docker.internal:9000" || third.RelayPort != "9000" || !third.Backup {
		t.Fatalf("third server = %+v", third)
	}
}

func TestParseServersRejectsInvalidOptions(t *testing.T) {
	for _, input := range []string{
		"app:8080 weight=0",
		"app:8080 weight",
		"app:8080 max_fails=-1",
		"app:8080 fail_timeout=soon",
		"app:8080 mystery=1",
		"",
	} {
		if _, err := ParseServers(input); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestRelayPortsAreDeduplicated(t *testing.T) {
	data := ConfigData{Servers: []Server{
		{Address: "host.docker.internal:8080", RelayPort: "8080"},
		{Address: "host.docker.internal:8080", RelayPort: "8080"},
		{Address: "host.docker.internal:9000", RelayPort: "9000"},
		{Address: "app:8080"},
	}}

	ports := data.RelayPorts()
	if len(ports) != 2 || ports[0] != "8080" || ports[1] != "9000" {
		t.Fatalf("relay ports = %v", ports)
	}
}

func TestNormalizeUpstream(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		upstream  string
		relayPort string
		resolve   bool
	}{
		{
			name:      "loopback ipv4",
			input:     "127.0.0.1:8080",
			upstream:  "host.docker.internal:8080",
			relayPort: "8080",
		},
		{
			name:      "localhost",
			input:     "localhost:3000",
			upstream:  "host.docker.internal:3000",
			relayPort: "3000",
		},
		{
			name:      "loopback ipv6",
			input:     "[::1]:9000",
			upstream:  "host.docker.internal:9000",
			relayPort: "9000",
		},
		{
			name:      "loopback with scheme",
			input:     "http://localhost:8080/",
			upstream:  "host.docker.internal:8080",
			relayPort: "8080",
		},
		{
			name:     "container name",
			input:    "grippy_backend:8080",
			upstream: "grippy_backend:8080",
			resolve:  true,
		},
		{
			name:     "private ip",
			input:    "172.22.0.4:80",
			upstream: "172.22.0.4:80",
		},
		{
			name:     "host docker internal",
			input:    "host.docker.internal:8080",
			upstream: "host.docker.internal:8080",
		},
		{
			name:     "hostname without port",
			input:    "example.com",
			upstream: "example.com",
			resolve:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream, relayPort, resolve := NormalizeUpstream(tt.input)
			if upstream != tt.upstream {
				t.Fatalf("upstream = %q, want %q", upstream, tt.upstream)
			}
			if relayPort != tt.relayPort {
				t.Fatalf("relayPort = %q, want %q", relayPort, tt.relayPort)
			}
			if resolve != tt.resolve {
				t.Fatalf("resolve = %v, want %v", resolve, tt.resolve)
			}
		})
	}
}
