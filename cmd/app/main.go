package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"EdgeProxy/internal/cli"
	"EdgeProxy/internal/deploy"
	"EdgeProxy/internal/nginx"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
)

var (
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func main() {
	domainFlag := flag.String("domain", "", "comma-separated domain(s); enables non-interactive mode")
	typeFlag := flag.String("type", "proxy", "service type: proxy, websocket, stream, redirect")
	upstreamFlag := flag.String("upstream", "", "upstream host:port")
	redirectFlag := flag.String("redirect", "", "redirect target (with -type=redirect)")
	bodySizeFlag := flag.String("body-size", "10M", "max request body size")
	sslFlag := flag.Bool("ssl", false, "issue a Let's Encrypt certificate")
	yesFlag := flag.Bool("yes", false, "skip the confirmation prompt")
	flag.Parse()

	if _, err := os.Stat("docker-compose.yml"); err != nil {
		fmt.Println(errStyle.Render("WARNING: docker-compose.yml is missing. Create it before running EdgeProxy."))
	}

	if _, err := os.Stat(".env"); err != nil {
		fmt.Println(errStyle.Render("WARNING: .env is missing. Create it with EMAIL=you@example.com before deployment."))
	}

	_ = godotenv.Load(".env")
	email := os.Getenv("EMAIL")
	if email == "" {
		fmt.Println(errStyle.Render("✖ EMAIL is not set in .env"))
		os.Exit(1)
	}
	if email == "you@example.com" {
		fmt.Println(errStyle.Render("WARNING: EMAIL is still the sample value. Update .env to a real email before issuing certificates."))
	}

	edgeDir, _ := os.Getwd()

	fmt.Printf("\n  %s %s\n", titleStyle.Render("EDGEPROXY"), dimStyle.Render("v1.0.0"))
	fmt.Printf("  %s\n\n", dimStyle.Render("Add a new domain to the reverse proxy"))

	var data nginx.ConfigData
	var err error
	if *domainFlag != "" {
		data, err = cli.BuildConfig(*domainFlag, *typeFlag, *upstreamFlag, *redirectFlag, *bodySizeFlag, *sslFlag)
	} else {
		data, err = cli.RunInteractive()
	}
	if err != nil {
		fmt.Println(errStyle.Render("✖ Aborted: " + err.Error()))
		os.Exit(1)
	}

	fmt.Printf("  %s\n", lipgloss.NewStyle().Bold(true).Render("Configuration Summary"))
	fmt.Printf("  ➜  Domain:    %s\n", data.Domains)
	fmt.Printf("  ➜  Type:      %s\n", data.Type)
	fmt.Printf("  ➜  Upstream:  %s\n", data.Upstream)
	if data.RelayPort != "" {
		fmt.Printf("  ➜  Host relay: 127.0.0.1:%s -> host.docker.internal:%s\n", data.RelayPort, data.RelayPort)
	}
	fmt.Printf("  ➜  SSL:       %v\n\n", data.SSL)

	var confirm = *yesFlag
	if !confirm {
		huh.NewConfirm().Title("Proceed with deployment?").Value(&confirm).Run()
	}
	if !confirm {
		fmt.Println("  Aborted.")
		os.Exit(0)
	}

	mgr := deploy.NewManager(edgeDir, email)
	var deployErr error
	_ = spinner.New().
		Title("Deploying configuration & issuing SSL...").
		Action(func() {
			deployErr = mgr.Deploy(data)
		}).
		Run()

	if deployErr != nil {
		fmt.Println(errStyle.Render("\n  ✖ Deployment failed: " + deployErr.Error()))
		os.Exit(1)
	}

	fmt.Println(successStyle.Render("\n  ✔ Ready! Domain is now live."))
	fmt.Printf("  ➜  URL:    %s\n", lipgloss.NewStyle().Underline(true).Render("https://"+data.Primary))
	fmt.Printf("  ➜  Config: %s\n\n", filepath.Join(edgeDir, "nginx/conf.d", data.Primary+".conf"))
}
