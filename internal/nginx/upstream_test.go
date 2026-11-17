package nginx

import "testing"

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
