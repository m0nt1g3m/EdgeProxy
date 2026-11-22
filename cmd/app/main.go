package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"EdgeProxy/internal/cli"
	"EdgeProxy/internal/deploy"
	"EdgeProxy/internal/nginx"
	"EdgeProxy/internal/state"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
)

// Set by GoReleaser at build time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

var (
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	boldStyle    = lipgloss.NewStyle().Bold(true)
)

type options struct {
	domain    string
	edit      string
	editRaw   string
	del       string
	list      bool
	service   string
	upstream  string
	redirect  string
	bodySize  string
	ssl       bool
	yes       bool
	balance   string
	keepalive string
	connectTO string
	readTO    string
	sendTO    string
	tries     string
	rateLimit string
	rateBurst string
	connLimit string
	cacheTTL  string

	visited map[string]bool
}

func main() {
	var opts options

	flag.StringVar(&opts.domain, "domain", "", "comma-separated domain(s) for a new site")
	flag.StringVar(&opts.edit, "edit", "", "edit an existing site by primary domain")
	flag.StringVar(&opts.editRaw, "edit-config", "", "edit the generated nginx config for a site by hand")
	flag.StringVar(&opts.del, "delete", "", "delete a site by primary domain")
	flag.BoolVar(&opts.list, "list", false, "list configured sites")
	flag.StringVar(&opts.service, "type", "", "service type: proxy, websocket, stream, redirect (default proxy)")
	flag.StringVar(&opts.upstream, "upstream", "", "upstream list: host:port[,host:port ...] with optional weight=, max_fails=, fail_timeout=, backup, down")
	flag.StringVar(&opts.redirect, "redirect", "", "redirect target (with -type=redirect)")
	flag.StringVar(&opts.bodySize, "body-size", "", "max request body size (default 10M, 0 = unlimited)")
	flag.BoolVar(&opts.ssl, "ssl", false, "issue or refresh a Let's Encrypt certificate")
	flag.BoolVar(&opts.yes, "yes", false, "skip confirmation prompts")
	flag.StringVar(&opts.balance, "balance", "", "load balancing: least_conn, round_robin, ip_hash, hash_uri, random_two (default least_conn)")
	flag.StringVar(&opts.keepalive, "keepalive", "", "upstream keepalive pool size (default 64, 0 disables)")
	flag.StringVar(&opts.connectTO, "connect-timeout", "", "upstream connect timeout (default 5s)")
	flag.StringVar(&opts.readTO, "read-timeout", "", "upstream read timeout (default 60s, 3600s for websocket/stream)")
	flag.StringVar(&opts.sendTO, "send-timeout", "", "upstream send timeout (default 60s)")
	flag.StringVar(&opts.tries, "tries", "", "upstream attempts per request, including the first (default 3)")
	flag.StringVar(&opts.rateLimit, "rate-limit", "", "requests per second per IP (0 = disabled)")
	flag.StringVar(&opts.rateBurst, "rate-burst", "", "rate limit burst (default 20 when a rate is set)")
	flag.StringVar(&opts.connLimit, "conn-limit", "", "concurrent connections per IP (0 = disabled)")
	flag.StringVar(&opts.cacheTTL, "cache-ttl", "", "response cache TTL for proxy sites (empty = disabled)")
	versionFlag := flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("edgeproxy %s (%s, %s)\n", Version, Commit, Date)
		return
	}

	opts.visited = make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { opts.visited[f.Name] = true })

	if err := validateActions(opts); err != nil {
		fmt.Println(errStyle.Render("✖ " + err.Error()))
		os.Exit(1)
	}

	if _, err := os.Stat("docker-compose.yml"); err != nil {
		fmt.Println(errStyle.Render("WARNING: docker-compose.yml is missing. Create it before running EdgeProxy."))
	}

	if _, err := os.Stat(".env"); err != nil {
		fmt.Println(errStyle.Render("WARNING: .env is missing. Create it with EMAIL=you@example.com before deployment."))
	}

	_ = godotenv.Load(".env")
	email := os.Getenv("EMAIL")
	if email == "" {
		fmt.Println(errStyle.Render("WARNING: EMAIL is not set. Sites without TLS can still be deployed, but certificates cannot be issued."))
	}
	if email == "you@example.com" {
		fmt.Println(errStyle.Render("WARNING: EMAIL is still the sample value. Update .env to a real email before issuing certificates."))
	}

	edgeDir, _ := os.Getwd()
	manager := deploy.NewManager(edgeDir, email)

	fmt.Printf("\n  %s %s\n", titleStyle.Render("EDGEPROXY"), dimStyle.Render(Version))
	fmt.Printf("  %s\n\n", dimStyle.Render("Reverse proxy manager with load balancing and failover"))

	if err := run(manager, opts); err != nil {
		fmt.Println(errStyle.Render("\n  ✖ " + err.Error()))
		os.Exit(1)
	}
}

func run(manager *deploy.Manager, opts options) error {
	switch {
	case opts.list:
		return listSites(manager)

	case opts.del != "":
		return deleteSite(manager, opts)

	case opts.editRaw != "":
		return editConfigManually(manager, opts.editRaw)

	case opts.edit != "":
		return editSite(manager, opts)

	case opts.domain != "":
		input := cli.Input{
			Domains:           opts.domain,
			Type:              opts.service,
			Upstreams:         opts.upstream,
			RedirectTo:        opts.redirect,
			BodySize:          opts.bodySize,
			SSL:               opts.ssl,
			Balance:           opts.balance,
			Keepalive:         opts.keepalive,
			ConnectTimeout:    opts.connectTO,
			ReadTimeout:       opts.readTO,
			SendTimeout:       opts.sendTO,
			NextUpstreamTries: opts.tries,
			RateLimit:         opts.rateLimit,
			RateBurst:         opts.rateBurst,
			ConnLimit:         opts.connLimit,
			CacheTTL:          opts.cacheTTL,
		}

		data, err := cli.BuildConfig(input)
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		return deployWithConfirmation(manager, data, nil, opts.yes)

	default:
		return runInteractive(manager)
	}
}

func runInteractive(manager *deploy.Manager) error {
	action, err := cli.RunMenu()
	if err != nil {
		return fmt.Errorf("aborted: %w", err)
	}

	switch action {
	case "add":
		data, err := cli.RunAddForm()
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		return deployWithConfirmation(manager, data, nil, false)

	case "edit":
		sites, err := manager.Store.List()
		if err != nil {
			return err
		}
		domain, err := cli.PickSite(siteOptions(sites))
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		site, err := manager.Store.Load(domain)
		if err != nil {
			return err
		}
		data, err := cli.RunEditForm(site.Data)
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		return deployWithConfirmation(manager, data, &site.Data, false)

	case "config":
		sites, err := manager.Store.List()
		if err != nil {
			return err
		}
		domain, err := cli.PickSite(siteOptions(sites))
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		return editConfigManually(manager, domain)

	case "delete":
		sites, err := manager.Store.List()
		if err != nil {
			return err
		}
		domain, err := cli.PickSite(siteOptions(sites))
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		confirmed, err := cli.Confirm(fmt.Sprintf("Delete %s? Its certificate files will be kept.", domain))
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		if !confirmed {
			fmt.Println("  Aborted.")
			return nil
		}
		if err := manager.Delete(domain); err != nil {
			return err
		}
		fmt.Println(successStyle.Render("\n  ✔ Site deleted and nginx reloaded."))
		return nil

	case "list":
		return listSites(manager)

	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func editSite(manager *deploy.Manager, opts options) error {
	site, err := manager.Store.Load(opts.edit)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return fmt.Errorf("no site found for %s", opts.edit)
		}
		return err
	}

	var data nginx.ConfigData
	if hasOverrides(opts.visited) || opts.yes {
		input := cli.InputFromConfig(site.Data)
		applyOverrides(&input, opts)
		data, err = cli.BuildConfig(input)
	} else {
		data, err = cli.RunEditForm(site.Data)
	}
	if err != nil {
		return fmt.Errorf("aborted: %w", err)
	}

	return deployWithConfirmation(manager, data, &site.Data, opts.yes)
}

func editConfigManually(manager *deploy.Manager, domain string) error {
	confPath := manager.Store.ConfPath(domain)
	if _, err := os.Stat(confPath); err != nil {
		return fmt.Errorf("no config found for %s", domain)
	}

	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		editor = "vi"
	}

	fmt.Printf("  Opening %s in %s.\n", confPath, editor)
	fmt.Println(dimStyle.Render("  Save and quit to validate the config and reload nginx. Quit without changes to cancel."))
	fmt.Println()

	result, err := manager.EditConfig(domain, cli.OpenEditor)
	if err != nil {
		return err
	}
	if !result.Changed {
		fmt.Println(dimStyle.Render("  No changes; nothing was reloaded."))
		return nil
	}

	fmt.Println(successStyle.Render("\n  ✔ Config applied and nginx reloaded."))
	if result.SettingsUpdated {
		fmt.Println(dimStyle.Render("  Site settings were refreshed from the edited config."))
	}
	if result.SettingsErr != nil {
		fmt.Println(errStyle.Render("  ! " + result.SettingsErr.Error()))
	}
	return nil
}

func deleteSite(manager *deploy.Manager, opts options) error {
	if !opts.yes {
		confirmed, err := cli.Confirm(fmt.Sprintf("Delete %s? Its certificate files will be kept.", opts.del))
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		if !confirmed {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	if err := manager.Delete(opts.del); err != nil {
		return err
	}

	fmt.Println(successStyle.Render("\n  ✔ Site deleted and nginx reloaded."))
	return nil
}

func deployWithConfirmation(manager *deploy.Manager, data nginx.ConfigData, previous *nginx.ConfigData, skipConfirm bool) error {
	printSummary(data)

	if !skipConfirm {
		confirmed, err := cli.Confirm("Proceed with deployment?")
		if err != nil {
			return fmt.Errorf("aborted: %w", err)
		}
		if !confirmed {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	var deployErr error
	_ = spinner.New().
		Title("Applying configuration & issuing certificates...").
		Action(func() {
			if previous != nil {
				deployErr = manager.DeployReplacing(*previous, data)
				return
			}
			deployErr = manager.Deploy(data)
		}).
		Run()

	if deployErr != nil {
		return fmt.Errorf("deployment failed: %w", deployErr)
	}

	scheme := "http"
	if data.SSL {
		scheme = "https"
	}

	fmt.Println(successStyle.Render("\n  ✔ Ready! Site is now live."))
	fmt.Printf("  ➜  URL:      %s\n", lipgloss.NewStyle().Underline(true).Render(scheme+"://"+data.Primary()))
	fmt.Printf("  ➜  Config:   %s\n", filepath.Join(manager.EdgeDir, "nginx", "conf.d", data.Primary()+".conf"))
	fmt.Printf("  ➜  Settings: %s\n\n", filepath.Join(manager.EdgeDir, "sites", data.Primary()+".json"))
	return nil
}

func printSummary(data nginx.ConfigData) {
	data.ApplyDefaults()

	fmt.Printf("  %s\n", boldStyle.Render("Configuration Summary"))
	fmt.Printf("  ➜  Domains:   %s\n", strings.Join(data.Domains, ", "))
	fmt.Printf("  ➜  Type:      %s\n", data.Type)
	if data.Type == nginx.TypeRedirect {
		fmt.Printf("  ➜  Redirect:  %s\n", data.RedirectTo)
	} else {
		fmt.Printf("  ➜  Upstreams:\n")
		for _, server := range data.Servers {
			fmt.Printf("       • %s\n", server.Directive())
		}
		fmt.Printf("  ➜  Balancing: %s\n", data.Balance)
		fmt.Printf("  ➜  Timeouts:  connect %s, read %s, send %s\n", data.ConnectTimeout, data.ReadTimeout, data.SendTimeout)
		fmt.Printf("  ➜  Retries:   %d attempts\n", data.NextUpstreamTries)
		if data.RateLimit > 0 {
			fmt.Printf("  ➜  Rate limit: %d req/s per IP (burst %d)\n", data.RateLimit, data.RateBurst)
		}
		if data.ConnLimit > 0 {
			fmt.Printf("  ➜  Max connections per IP: %d\n", data.ConnLimit)
		}
		if data.CacheTTL != "" {
			fmt.Printf("  ➜  Response cache: %s\n", data.CacheTTL)
		}
		ports := data.RelayPorts()
		if len(ports) > 0 {
			fmt.Printf("  ➜  Host relays: %s\n", strings.Join(ports, ", "))
		}
	}
	fmt.Printf("  ➜  SSL:       %v\n\n", data.SSL)
}

func listSites(manager *deploy.Manager) error {
	sites, err := manager.Store.List()
	if err != nil {
		return err
	}
	if len(sites) == 0 {
		fmt.Println(dimStyle.Render("  No sites configured yet."))
		return nil
	}

	sort.Slice(sites, func(i, j int) bool {
		return sites[i].Data.Primary() < sites[j].Data.Primary()
	})

	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "  DOMAIN\tTYPE\tTLS\tSOURCE\tTARGET")
	for _, site := range sites {
		if site.Err != nil {
			fmt.Fprintf(writer, "  %s\t-\t-\tunreadable\t%s\n", site.Data.Primary(), site.Err)
			continue
		}

		target := site.Data.RedirectTo
		if site.Data.Type != nginx.TypeRedirect {
			target = site.Data.UpstreamInput()
		}

		source := "managed"
		if !site.Managed {
			source = "imported"
		}

		fmt.Fprintf(writer, "  %s\t%s\t%v\t%s\t%s\n",
			site.Data.Primary(), site.Data.Type, site.Data.SSL, source, target)
	}
	return writer.Flush()
}

func siteOptions(sites []state.Site) []cli.SiteOption {
	options := make([]cli.SiteOption, 0, len(sites))
	for _, site := range sites {
		domain := site.Data.Primary()
		label := fmt.Sprintf("%s (%s)", domain, site.Data.Type)
		if !site.Managed {
			label += " [imported]"
		}
		if site.Err != nil {
			label += " [unreadable]"
		}
		options = append(options, cli.SiteOption{Domain: domain, Label: label})
	}
	return options
}

func applyOverrides(input *cli.Input, opts options) {
	if opts.visited["domain"] {
		input.Domains = opts.domain
	}
	if opts.visited["type"] {
		input.Type = opts.service
	}
	if opts.visited["upstream"] {
		input.Upstreams = opts.upstream
	}
	if opts.visited["redirect"] {
		input.RedirectTo = opts.redirect
	}
	if opts.visited["body-size"] {
		input.BodySize = opts.bodySize
	}
	if opts.visited["ssl"] {
		input.SSL = opts.ssl
	}
	if opts.visited["balance"] {
		input.Balance = opts.balance
	}
	if opts.visited["keepalive"] {
		input.Keepalive = opts.keepalive
	}
	if opts.visited["connect-timeout"] {
		input.ConnectTimeout = opts.connectTO
	}
	if opts.visited["read-timeout"] {
		input.ReadTimeout = opts.readTO
	}
	if opts.visited["send-timeout"] {
		input.SendTimeout = opts.sendTO
	}
	if opts.visited["tries"] {
		input.NextUpstreamTries = opts.tries
	}
	if opts.visited["rate-limit"] {
		input.RateLimit = opts.rateLimit
	}
	if opts.visited["rate-burst"] {
		input.RateBurst = opts.rateBurst
	}
	if opts.visited["conn-limit"] {
		input.ConnLimit = opts.connLimit
	}
	if opts.visited["cache-ttl"] {
		input.CacheTTL = opts.cacheTTL
	}
}

func validateActions(opts options) error {
	var actions []string
	if opts.edit != "" {
		actions = append(actions, "-edit")
	} else if opts.editRaw != "" {
		actions = append(actions, "-edit-config")
	} else if opts.domain != "" {
		actions = append(actions, "-domain")
	}
	if opts.del != "" {
		actions = append(actions, "-delete")
	}
	if opts.list {
		actions = append(actions, "-list")
	}

	if len(actions) > 1 {
		return fmt.Errorf("%s cannot be used together", strings.Join(actions, " and "))
	}
	return nil
}

func hasOverrides(visited map[string]bool) bool {
	for _, name := range []string{
		"domain", "type", "upstream", "redirect", "body-size", "ssl",
		"balance", "keepalive", "connect-timeout", "read-timeout", "send-timeout",
		"tries", "rate-limit", "rate-burst", "conn-limit", "cache-ttl",
	} {
		if visited[name] {
			return true
		}
	}
	return false
}
