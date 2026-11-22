package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"EdgeProxy/internal/nginx"

	"github.com/charmbracelet/huh"
)

// Input holds the raw values collected from flags or the interactive form.
// Numeric and duration values stay as strings so an empty value can mean
// "keep the default".
type Input struct {
	Domains           string
	Type              string
	Upstreams         string
	RedirectTo        string
	BodySize          string
	SSL               bool
	Balance           string
	Keepalive         string
	ConnectTimeout    string
	ReadTimeout       string
	SendTimeout       string
	NextUpstreamTries string
	RateLimit         string
	RateBurst         string
	ConnLimit         string
	CacheTTL          string
}

// BuildConfig validates raw input and turns it into a complete site.
func BuildConfig(in Input) (nginx.ConfigData, error) {
	domains := splitDomains(in.Domains)
	if len(domains) == 0 {
		return nginx.ConfigData{}, fmt.Errorf("at least one domain is required")
	}
	for _, domain := range domains {
		if !ValidDomain(domain) {
			return nginx.ConfigData{}, fmt.Errorf("invalid domain %q", domain)
		}
	}

	kind := strings.TrimSpace(in.Type)
	if kind == "" {
		kind = nginx.TypeProxy
	}
	if !nginx.ValidType(kind) {
		return nginx.ConfigData{}, fmt.Errorf("unknown service type %q", kind)
	}

	balance := strings.TrimSpace(in.Balance)
	if balance == "" {
		balance = nginx.BalanceLeastConn
	}
	if !nginx.ValidBalance(balance) {
		return nginx.ConfigData{}, fmt.Errorf("unknown balancing method %q", balance)
	}

	connectTimeout, err := durationField(in.ConnectTimeout, nginx.DefaultConnectTimeout, "connect timeout")
	if err != nil {
		return nginx.ConfigData{}, err
	}
	sendTimeout, err := durationField(in.SendTimeout, nginx.DefaultSendTimeout, "send timeout")
	if err != nil {
		return nginx.ConfigData{}, err
	}

	readDefault := nginx.DefaultReadTimeout
	if kind == nginx.TypeWebSocket || kind == nginx.TypeStream {
		readDefault = nginx.DefaultStreamReadTimeout
	}
	readTimeout, err := durationField(in.ReadTimeout, readDefault, "read timeout")
	if err != nil {
		return nginx.ConfigData{}, err
	}

	keepalive, err := intField(in.Keepalive, nginx.DefaultKeepalive, 0, "keepalive connections")
	if err != nil {
		return nginx.ConfigData{}, err
	}
	tries, err := intField(in.NextUpstreamTries, nginx.DefaultNextUpstreamTries, 1, "retry attempts")
	if err != nil {
		return nginx.ConfigData{}, err
	}
	rateLimit, err := intField(in.RateLimit, 0, 0, "rate limit")
	if err != nil {
		return nginx.ConfigData{}, err
	}
	rateBurst, err := intField(in.RateBurst, 0, 0, "rate limit burst")
	if err != nil {
		return nginx.ConfigData{}, err
	}
	connLimit, err := intField(in.ConnLimit, 0, 0, "connection limit")
	if err != nil {
		return nginx.ConfigData{}, err
	}
	if rateLimit > 0 && rateBurst == 0 {
		rateBurst = nginx.DefaultRateBurst
	}

	cacheTTL := strings.TrimSpace(in.CacheTTL)
	if cacheTTL != "" {
		if kind != nginx.TypeProxy {
			return nginx.ConfigData{}, fmt.Errorf("response caching is only supported for proxy sites")
		}
		if !nginx.ValidDuration(cacheTTL) {
			return nginx.ConfigData{}, fmt.Errorf("invalid cache TTL %q", cacheTTL)
		}
	}

	bodySize := strings.TrimSpace(in.BodySize)
	if kind == nginx.TypeStream {
		bodySize = "0"
	} else {
		if bodySize == "" {
			bodySize = nginx.DefaultBodySize
		}
		if !nginx.ValidSize(bodySize) {
			return nginx.ConfigData{}, fmt.Errorf("invalid max body size %q", bodySize)
		}
	}

	data := nginx.ConfigData{
		Domains:            domains,
		Type:               kind,
		BodySize:           bodySize,
		SSL:                in.SSL,
		Balance:            balance,
		Keepalive:          keepalive,
		ConnectTimeout:     connectTimeout,
		ReadTimeout:        readTimeout,
		SendTimeout:        sendTimeout,
		NextUpstreamTries:  tries,
		NextUpstreamWindow: nginx.DefaultNextUpstreamWindow,
		RateLimit:          rateLimit,
		RateBurst:          rateBurst,
		ConnLimit:          connLimit,
		CacheTTL:           cacheTTL,
	}

	if kind == nginx.TypeRedirect {
		target, err := normalizeRedirectTarget(in.RedirectTo)
		if err != nil {
			return nginx.ConfigData{}, err
		}
		data.RedirectTo = target
	} else {
		servers, err := nginx.ParseServers(in.Upstreams)
		if err != nil {
			return nginx.ConfigData{}, err
		}
		data.Servers = servers
		if data.HasBackupServer() && !data.SupportsBackup() {
			return nginx.ConfigData{}, fmt.Errorf("backup servers are only supported with round_robin and least_conn balancing")
		}
	}

	data.ApplyDefaults()
	return data, nil
}

// InputFromConfig turns a saved site back into form values.
func InputFromConfig(data nginx.ConfigData) Input {
	data.ApplyDefaults()

	return Input{
		Domains:           strings.Join(data.Domains, ", "),
		Type:              data.Type,
		Upstreams:         data.UpstreamInput(),
		RedirectTo:        data.RedirectTo,
		BodySize:          data.BodySize,
		SSL:               data.SSL,
		Balance:           data.Balance,
		Keepalive:         strconv.Itoa(data.Keepalive),
		ConnectTimeout:    data.ConnectTimeout,
		ReadTimeout:       data.ReadTimeout,
		SendTimeout:       data.SendTimeout,
		NextUpstreamTries: strconv.Itoa(data.NextUpstreamTries),
		RateLimit:         strconv.Itoa(data.RateLimit),
		RateBurst:         strconv.Itoa(data.RateBurst),
		ConnLimit:         strconv.Itoa(data.ConnLimit),
		CacheTTL:          data.CacheTTL,
	}
}

// RunAddForm asks for a new site.
func RunAddForm() (nginx.ConfigData, error) {
	return runForm(Input{
		BodySize:          nginx.DefaultBodySize,
		Balance:           nginx.BalanceLeastConn,
		Keepalive:         strconv.Itoa(nginx.DefaultKeepalive),
		ConnectTimeout:    nginx.DefaultConnectTimeout,
		SendTimeout:       nginx.DefaultSendTimeout,
		NextUpstreamTries: strconv.Itoa(nginx.DefaultNextUpstreamTries),
		RateLimit:         "0",
		RateBurst:         "0",
		ConnLimit:         "0",
	})
}

// RunEditForm asks for parameters of an existing site, prefilled with its
// current values.
func RunEditForm(data nginx.ConfigData) (nginx.ConfigData, error) {
	return runForm(InputFromConfig(data))
}

func runForm(in Input) (nginx.ConfigData, error) {
	var (
		domains    = in.Domains
		kind       = in.Type
		upstreams  = in.Upstreams
		redirectTo = in.RedirectTo
		bodySize   = in.BodySize
		balance    = in.Balance
		keepalive  = in.Keepalive
		connectTO  = in.ConnectTimeout
		readTO     = in.ReadTimeout
		sendTO     = in.SendTimeout
		tries      = in.NextUpstreamTries
		rateLimit  = in.RateLimit
		rateBurst  = in.RateBurst
		connLimit  = in.ConnLimit
		cacheTTL   = in.CacheTTL
		ssl        = in.SSL
	)

	if kind == "" {
		kind = nginx.TypeProxy
	}
	if balance == "" {
		balance = nginx.BalanceLeastConn
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Domain(s)").
				Description("Comma-separated, the first one names the config and certificate").
				Placeholder("example.com, www.example.com").
				Value(&domains).
				Validate(validateDomains),
			huh.NewSelect[string]().
				Title("Service type").
				Options(
					huh.NewOption("Standard reverse proxy", nginx.TypeProxy),
					huh.NewOption("WebSocket", nginx.TypeWebSocket),
					huh.NewOption("Streaming (no buffering)", nginx.TypeStream),
					huh.NewOption("Redirect", nginx.TypeRedirect),
				).
				Value(&kind),
		).Title("Basics"),

		huh.NewGroup(
			huh.NewInput().
				Title("Upstreams").
				Description("Comma-separated. Options: weight=2, max_fails=3, fail_timeout=10s, backup, down. backup works only with round robin and least_conn.").
				Placeholder("app1:8080, app2:8080 weight=2, 127.0.0.1:9000 backup").
				Value(&upstreams).
				Validate(validateUpstreams),
		).Title("Routing").WithHideFunc(func() bool { return kind == nginx.TypeRedirect }),

		huh.NewGroup(
			huh.NewInput().
				Title("Redirect target").
				Description("Absolute http(s) URL; path and query are preserved").
				Placeholder("https://example.com").
				Value(&redirectTo).
				Validate(validateRedirect),
		).Title("Routing").WithHideFunc(func() bool { return kind != nginx.TypeRedirect }),

		huh.NewGroup(
			huh.NewInput().
				Title("Max body size").
				Description("For example 10M, 100M, or 0 for unlimited").
				Value(&bodySize).
				Validate(validateSize),
			huh.NewSelect[string]().
				Title("Load balancing").
				Description("least_conn spreads load by active connections; ip_hash keeps clients sticky").
				Options(
					huh.NewOption("Least connections (recommended)", nginx.BalanceLeastConn),
					huh.NewOption("Round robin", nginx.BalanceRoundRobin),
					huh.NewOption("IP hash (sticky sessions)", nginx.BalanceIPHash),
					huh.NewOption("Consistent hash by request URI", nginx.BalanceHashURI),
					huh.NewOption("Random two, least connections", nginx.BalanceRandomTwo),
				).
				Value(&balance),
			huh.NewInput().
				Title("Upstream keepalive pool").
				Description("Idle connections per worker; 0 disables the pool").
				Value(&keepalive).
				Validate(validateNonNegative),
		).Title("Load balancing & resilience").WithHideFunc(func() bool { return kind == nginx.TypeRedirect }),

		huh.NewGroup(
			huh.NewInput().
				Title("Connect timeout").
				Description("How long to wait for an upstream TCP connection (default 5s)").
				Value(&connectTO).
				Validate(validateDuration),
			huh.NewInput().
				Title("Read timeout").
				Description("Upstream response timeout; websocket/stream default to 3600s").
				Placeholder("60s").
				Value(&readTO).
				Validate(validateOptionalDuration),
			huh.NewInput().
				Title("Send timeout").
				Description("Timeout between writes to the upstream (default 60s)").
				Value(&sendTO).
				Validate(validateDuration),
			huh.NewInput().
				Title("Retry attempts").
				Description("Total upstream attempts per request, including the first one (default 3)").
				Value(&tries).
				Validate(validatePositive),
		).Title("Timeouts & failover").WithHideFunc(func() bool { return kind == nginx.TypeRedirect }),

		huh.NewGroup(
			huh.NewInput().
				Title("Rate limit (requests/second per IP)").
				Description("0 disables rate limiting").
				Value(&rateLimit).
				Validate(validateNonNegative),
			huh.NewInput().
				Title("Rate limit burst").
				Description("Requests allowed to burst above the rate; 0 uses 20 when a rate is set").
				Value(&rateBurst).
				Validate(validateNonNegative),
			huh.NewInput().
				Title("Concurrent connections per IP").
				Description("0 disables connection limiting").
				Value(&connLimit).
				Validate(validateNonNegative),
			huh.NewInput().
				Title("Response cache TTL").
				Description("Proxy only. Empty disables caching; authenticated and cookie-bearing requests always bypass it.").
				Placeholder("off, 30s, 5m").
				Value(&cacheTTL).
				Validate(validateOptionalDuration),
		).Title("Protection").WithHideFunc(func() bool { return kind == nginx.TypeRedirect }),

		huh.NewGroup(
			huh.NewConfirm().
				Title("Issue or refresh a Let's Encrypt certificate?").
				Value(&ssl),
		).Title("TLS"),
	)

	if err := form.Run(); err != nil {
		return nginx.ConfigData{}, err
	}

	return BuildConfig(Input{
		Domains:           domains,
		Type:              kind,
		Upstreams:         upstreams,
		RedirectTo:        redirectTo,
		BodySize:          bodySize,
		SSL:               ssl,
		Balance:           balance,
		Keepalive:         keepalive,
		ConnectTimeout:    connectTO,
		ReadTimeout:       readTO,
		SendTimeout:       sendTO,
		NextUpstreamTries: tries,
		RateLimit:         rateLimit,
		RateBurst:         rateBurst,
		ConnLimit:         connLimit,
		CacheTTL:          cacheTTL,
	})
}

// SiteOption is one entry in the edit/delete picker.
type SiteOption struct {
	Domain string
	Label  string
}

// PickSite asks which site to manage.
func PickSite(options []SiteOption) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no sites found")
	}

	choices := make([]huh.Option[string], 0, len(options))
	for _, option := range options {
		choices = append(choices, huh.NewOption(option.Label, option.Domain))
	}

	var domain string
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Select a site").
			Options(choices...).
			Value(&domain),
	)).Run()
	if err != nil {
		return "", err
	}
	return domain, nil
}

// RunMenu asks what the user wants to do when no action flags were given.
func RunMenu() (string, error) {
	var action string
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("What do you want to do?").
			Options(
				huh.NewOption("Add a new site", "add"),
				huh.NewOption("Edit site parameters", "edit"),
				huh.NewOption("Edit nginx config manually", "config"),
				huh.NewOption("Delete a site", "delete"),
				huh.NewOption("List sites", "list"),
			).
			Value(&action),
	)).Run()
	if err != nil {
		return "", err
	}
	return action, nil
}

// Confirm asks a yes/no question.
func Confirm(title string) (bool, error) {
	var confirmed bool
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Value(&confirmed),
	)).Run()
	return confirmed, err
}

func splitDomains(raw string) []string {
	seen := make(map[string]bool)
	var domains []string
	for _, domain := range strings.Split(raw, ",") {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		domains = append(domains, domain)
	}
	return domains
}

// ValidDomain reports whether value is a syntactically valid DNS name.
func ValidDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}

	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		for i, r := range label {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '-' && i != 0 && i != len(label)-1:
			default:
				return false
			}
		}
	}

	return true
}

func normalizeRedirectTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return "", fmt.Errorf("redirect target must be an absolute http(s) URL")
	}
	if target.RawQuery != "" || target.Fragment != "" {
		return "", fmt.Errorf("redirect target must not contain a query or fragment; the original request path is preserved")
	}
	return strings.TrimSuffix(raw, "/"), nil
}

func durationField(raw, fallback, name string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = fallback
	}
	if !nginx.ValidDuration(value) {
		return "", fmt.Errorf("invalid %s %q", name, value)
	}
	return value, nil
}

func intField(raw string, fallback, minimum int, name string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < minimum {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return number, nil
}

func validateDomains(value string) error {
	domains := splitDomains(value)
	if len(domains) == 0 {
		return fmt.Errorf("at least one domain is required")
	}
	for _, domain := range domains {
		if !ValidDomain(domain) {
			return fmt.Errorf("invalid domain %q", domain)
		}
	}
	return nil
}

func validateUpstreams(value string) error {
	_, err := nginx.ParseServers(value)
	return err
}

func validateRedirect(value string) error {
	_, err := normalizeRedirectTarget(value)
	return err
}

func validateSize(value string) error {
	if !nginx.ValidSize(value) {
		return fmt.Errorf("use a size such as 10M, 1G, or 0")
	}
	return nil
}

func validateDuration(value string) error {
	if !nginx.ValidDuration(value) {
		return fmt.Errorf("use a duration such as 5s, 1m, or 1h")
	}
	return nil
}

func validateOptionalDuration(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return validateDuration(value)
}

func validateNonNegative(value string) error {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number < 0 {
		return fmt.Errorf("enter a non-negative number")
	}
	return nil
}

func validatePositive(value string) error {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number < 1 {
		return fmt.Errorf("enter a number greater than zero")
	}
	return nil
}
