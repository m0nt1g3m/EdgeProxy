package nginx

import (
	"fmt"
	"regexp"
	"strings"
)

// Service types.
const (
	TypeProxy     = "proxy"
	TypeWebSocket = "websocket"
	TypeStream    = "stream"
	TypeRedirect  = "redirect"
)

// Load-balancing methods.
const (
	BalanceRoundRobin = "round_robin"
	BalanceLeastConn  = "least_conn"
	BalanceIPHash     = "ip_hash"
	BalanceHashURI    = "hash_uri"
	BalanceRandomTwo  = "random_two"
)

// Defaults for new sites. They are tuned for failover: a failed upstream is
// ejected quickly and requests are retried on the next available server.
const (
	DefaultBodySize           = "10M"
	DefaultKeepalive          = 64
	DefaultConnectTimeout     = "5s"
	DefaultReadTimeout        = "60s"
	DefaultStreamReadTimeout  = "3600s"
	DefaultSendTimeout        = "60s"
	DefaultNextUpstreamTries  = 3
	DefaultNextUpstreamWindow = "10s"
	DefaultMaxFails           = 3
	DefaultFailTimeout        = "10s"
	DefaultRateBurst          = 20
)

// Server is one upstream backend.
type Server struct {
	Address     string `json:"address"`
	Weight      int    `json:"weight,omitempty"`
	MaxFails    int    `json:"max_fails,omitempty"`
	FailTimeout string `json:"fail_timeout,omitempty"`
	Backup      bool   `json:"backup,omitempty"`
	Down        bool   `json:"down,omitempty"`
	Resolve     bool   `json:"resolve,omitempty"`
	RelayPort   string `json:"relay_port,omitempty"`
}

// Directive renders the server as an nginx upstream entry.
func (s Server) Directive() string {
	parts := []string{s.Address}

	if s.Weight > 1 {
		parts = append(parts, fmt.Sprintf("weight=%d", s.Weight))
	}
	if s.FailTimeout != "" {
		parts = append(parts,
			fmt.Sprintf("max_fails=%d", s.MaxFails),
			"fail_timeout="+s.FailTimeout,
		)
	}
	if s.Backup {
		parts = append(parts, "backup")
	}
	if s.Down {
		parts = append(parts, "down")
	}
	if s.Resolve {
		parts = append(parts, "resolve")
	}

	return strings.Join(parts, " ")
}

// InputString renders the server in the same compact syntax accepted by
// ParseServers, so it can be shown in the edit form.
func (s Server) InputString() string {
	parts := []string{s.Address}

	if s.Weight > 1 {
		parts = append(parts, fmt.Sprintf("weight=%d", s.Weight))
	}
	if s.FailTimeout != "" {
		parts = append(parts,
			fmt.Sprintf("max_fails=%d", s.MaxFails),
			"fail_timeout="+s.FailTimeout,
		)
	}
	if s.Backup {
		parts = append(parts, "backup")
	}
	if s.Down {
		parts = append(parts, "down")
	}

	return strings.Join(parts, " ")
}

// ConfigData is the complete description of one proxied site. It is both the
// template input and the persisted site record.
type ConfigData struct {
	Domains            []string `json:"domains"`
	Type               string   `json:"type"`
	Servers            []Server `json:"servers,omitempty"`
	RedirectTo         string   `json:"redirect_to,omitempty"`
	BodySize           string   `json:"body_size"`
	SSL                bool     `json:"ssl"`
	Balance            string   `json:"balance,omitempty"`
	Keepalive          int      `json:"keepalive,omitempty"`
	ConnectTimeout     string   `json:"connect_timeout,omitempty"`
	ReadTimeout        string   `json:"read_timeout,omitempty"`
	SendTimeout        string   `json:"send_timeout,omitempty"`
	NextUpstreamTries  int      `json:"next_upstream_tries,omitempty"`
	NextUpstreamWindow string   `json:"next_upstream_timeout,omitempty"`
	RateLimit          int      `json:"rate_limit,omitempty"`
	RateBurst          int      `json:"rate_burst,omitempty"`
	ConnLimit          int      `json:"conn_limit,omitempty"`
	CacheTTL           string   `json:"cache_ttl,omitempty"`
}

// Primary returns the first domain. It names the generated config, the
// certificate lineage and the saved site record.
func (c ConfigData) Primary() string {
	if len(c.Domains) == 0 {
		return ""
	}
	return c.Domains[0]
}

// ServerNames returns the nginx server_name value.
func (c ConfigData) ServerNames() string {
	return strings.Join(c.Domains, " ")
}

// UpstreamName returns the nginx upstream/zone name for this site.
func (c ConfigData) UpstreamName() string {
	return UpstreamName(c.Primary())
}

// HasUpstream reports whether the site proxies traffic.
func (c ConfigData) HasUpstream() bool {
	return c.Type != TypeRedirect
}

// BalanceDirective renders the configured load-balancing method. An empty
// result means nginx's default round robin.
func (c ConfigData) BalanceDirective() string {
	switch c.Balance {
	case BalanceLeastConn:
		return "least_conn;"
	case BalanceIPHash:
		return "ip_hash;"
	case BalanceHashURI:
		return "hash $request_uri consistent;"
	case BalanceRandomTwo:
		return "random two least_conn;"
	default:
		return ""
	}
}

// SupportsBackup reports whether the chosen balancing method accepts
// "backup" servers. nginx only allows it for round robin and least_conn.
func (c ConfigData) SupportsBackup() bool {
	return c.Balance == "" || c.Balance == BalanceRoundRobin || c.Balance == BalanceLeastConn
}

// HasBackupServer reports whether any backend is marked as backup.
func (c ConfigData) HasBackupServer() bool {
	for _, server := range c.Servers {
		if server.Backup {
			return true
		}
	}
	return false
}

// RelayPorts returns the unique host loopback ports that need a socat relay.
func (c ConfigData) RelayPorts() []string {
	seen := make(map[string]struct{}, len(c.Servers))
	var ports []string
	for _, server := range c.Servers {
		if server.RelayPort == "" {
			continue
		}
		if _, ok := seen[server.RelayPort]; ok {
			continue
		}
		seen[server.RelayPort] = struct{}{}
		ports = append(ports, server.RelayPort)
	}
	return ports
}

// UpstreamInput renders all backends in the compact edit-form syntax.
func (c ConfigData) UpstreamInput() string {
	parts := make([]string, 0, len(c.Servers))
	for _, server := range c.Servers {
		parts = append(parts, server.InputString())
	}
	return strings.Join(parts, ", ")
}

// ApplyDefaults fills in missing values on records loaded from disk or built
// by hand. It never overrides explicit user choices.
func (c *ConfigData) ApplyDefaults() {
	for i := range c.Domains {
		c.Domains[i] = strings.TrimSpace(c.Domains[i])
	}

	if c.Type == "" {
		c.Type = TypeProxy
	}
	if c.BodySize == "" {
		if c.Type == TypeStream {
			c.BodySize = "0"
		} else {
			c.BodySize = DefaultBodySize
		}
	}
	if c.Balance == "" {
		c.Balance = BalanceLeastConn
	}
	if c.ConnectTimeout == "" {
		c.ConnectTimeout = DefaultConnectTimeout
	}
	if c.SendTimeout == "" {
		c.SendTimeout = DefaultSendTimeout
	}
	if c.ReadTimeout == "" {
		if c.Type == TypeWebSocket || c.Type == TypeStream {
			c.ReadTimeout = DefaultStreamReadTimeout
		} else {
			c.ReadTimeout = DefaultReadTimeout
		}
	}
	if c.NextUpstreamTries <= 0 {
		c.NextUpstreamTries = DefaultNextUpstreamTries
	}
	if c.NextUpstreamWindow == "" {
		c.NextUpstreamWindow = DefaultNextUpstreamWindow
	}
	if c.RateLimit > 0 && c.RateBurst <= 0 {
		c.RateBurst = DefaultRateBurst
	}

	for i := range c.Servers {
		if c.Servers[i].FailTimeout == "" && c.Servers[i].MaxFails == 0 {
			c.Servers[i].MaxFails = DefaultMaxFails
			c.Servers[i].FailTimeout = DefaultFailTimeout
		}
	}
}

// UpstreamName converts a domain into a stable nginx identifier.
func UpstreamName(domain string) string {
	return "up_" + strings.NewReplacer(".", "_", "-", "_", ":", "_").Replace(domain)
}

var (
	durationRE = regexp.MustCompile(`^\d+(ms|s|m|h|d|w|M|y)?$`)
	sizeRE     = regexp.MustCompile(`^(0|[1-9][0-9]*[kKmMgG]?)$`)
)

// ValidDuration reports whether value is an nginx time value.
func ValidDuration(value string) bool {
	return durationRE.MatchString(strings.TrimSpace(value))
}

// ValidSize reports whether value is an nginx size value such as 10M or 0.
func ValidSize(value string) bool {
	return sizeRE.MatchString(strings.TrimSpace(value))
}

// ValidBalance reports whether value is a supported balancing method.
func ValidBalance(value string) bool {
	switch value {
	case BalanceRoundRobin, BalanceLeastConn, BalanceIPHash, BalanceHashURI, BalanceRandomTwo:
		return true
	default:
		return false
	}
}

// ValidType reports whether value is a supported service type.
func ValidType(value string) bool {
	switch value {
	case TypeProxy, TypeWebSocket, TypeStream, TypeRedirect:
		return true
	default:
		return false
	}
}
