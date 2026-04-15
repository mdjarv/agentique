package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/allbin/agentique/backend/internal/config"
	"github.com/allbin/agentique/backend/internal/paths"
)

type summaryModel struct {
	cfg            *config.Config
	serviceInstall bool
}

func newSummaryModel(cfg *config.Config, serviceInstall bool) summaryModel {
	return summaryModel{cfg: cfg, serviceInstall: serviceInstall}
}

func (m summaryModel) View() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Setup complete"))
	b.WriteString("\n")

	row := func(key, val string) {
		fmt.Fprintf(&b, "  %s  %s\n", styleKey.Render(key), styleVal.Render(val))
	}

	row("Config", config.Path())
	row("Data", paths.DataDir())
	row("Address", m.cfg.Server.Addr)

	if m.cfg.Server.TLSCert != "" {
		row("TLS", "enabled")
	} else {
		row("TLS", styleDim.Render("disabled"))
	}

	if m.cfg.Server.DisableAuth {
		row("Auth", styleDim.Render("disabled"))
	} else {
		row("Auth", "passkey (WebAuthn)")
	}

	if m.cfg.Setup.InitialProject != "" {
		row("Project", m.cfg.Setup.InitialProject)
	}

	b.WriteString("\n")
	if m.serviceInstall {
		b.WriteString(styleDim.Render("  Service installed and started."))
	} else {
		scheme := "http"
		if m.cfg.Server.TLSCert != "" {
			scheme = "https"
		}
		host, port, _ := net.SplitHostPort(m.cfg.Server.Addr)
		if host == "" || host == "0.0.0.0" {
			host = "localhost"
		}
		url := fmt.Sprintf("%s://%s:%s", scheme, host, port)
		b.WriteString(fmt.Sprintf("  Start with: %s\n", styleVal.Render("agentique serve")))
		b.WriteString(fmt.Sprintf("  Then open:  %s", styleSelected.Render(url)))
	}

	b.WriteString("\n\n")
	b.WriteString(styleDim.Render("press any key to exit"))

	return b.String()
}
