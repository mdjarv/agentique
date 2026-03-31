package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/allbin/agentique/backend/internal/config"
	"github.com/allbin/agentique/backend/internal/doctor"
	"github.com/allbin/agentique/backend/internal/paths"
	"github.com/allbin/agentique/backend/internal/service"
)

func init() {
	rootCmd.AddCommand(setupCmd)
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Guided first-time configuration",
	RunE:  runSetup,
}

var setupReader *bufio.Reader

func runSetup(cmd *cobra.Command, args []string) error {
	setupReader = bufio.NewReader(os.Stdin)

	fmt.Println("Agentique Setup")
	fmt.Println("================")
	fmt.Println()

	// Step 1: Doctor checks.
	fmt.Println("Checking dependencies...")
	fmt.Println()
	checks := doctor.RunAll()
	printChecks(checks)
	fmt.Println()

	if doctor.HasFailures(checks) {
		fmt.Println("Fix the issues above before continuing.")
		return fmt.Errorf("required checks failed")
	}

	cfg := config.Default()

	// Step 2: Network mode.
	fmt.Println("How will you access Agentique?")
	choice := promptChoice([]string{
		"Localhost only (recommended for single user)",
		"Over the network (LAN, Tailscale, etc.)",
	}, 0)

	networkMode := choice == 1
	if networkMode {
		cfg.Server.Addr = "0.0.0.0:9201"
	} else {
		cfg.Server.Addr = "localhost:9201"
		cfg.Server.DisableAuth = true
	}

	// Step 3: TLS (if network).
	if networkMode {
		fmt.Println()
		fmt.Println("How will you handle HTTPS?")
		tlsChoice := promptChoice([]string{
			"I have TLS certificates",
			"Generate self-signed certificates",
			"I'll use a reverse proxy (nginx, caddy, etc.)",
		}, 2)

		switch tlsChoice {
		case 0: // existing certs
			cert := promptString("Path to certificate file: ")
			key := promptString("Path to key file: ")
			if _, err := os.Stat(cert); err != nil {
				return fmt.Errorf("certificate not found: %s", cert)
			}
			if _, err := os.Stat(key); err != nil {
				return fmt.Errorf("key not found: %s", key)
			}
			cfg.Server.TLSCert = cert
			cfg.Server.TLSKey = key

		case 1: // self-signed
			certDir := filepath.Join(paths.DataDir(), "certs")
			certPath := filepath.Join(certDir, "server.crt")
			keyPath := filepath.Join(certDir, "server.key")

			fmt.Printf("Generating self-signed certificate in %s...\n", certDir)
			if err := generateSelfSignedCert(certPath, keyPath); err != nil {
				return fmt.Errorf("generate certificate: %w", err)
			}
			fmt.Println("Certificate generated (valid for 365 days)")
			cfg.Server.TLSCert = certPath
			cfg.Server.TLSKey = keyPath

		case 2: // reverse proxy
			fmt.Println()
			fmt.Println("Configure your reverse proxy to forward to localhost:9201.")
			fmt.Println()
			fmt.Println("Example nginx config:")
			fmt.Println("  location / {")
			fmt.Println("    proxy_pass http://127.0.0.1:9201;")
			fmt.Println("    proxy_http_version 1.1;")
			fmt.Println("    proxy_set_header Upgrade $http_upgrade;")
			fmt.Println("    proxy_set_header Connection \"upgrade\";")
			fmt.Println("  }")
			fmt.Println()
			fmt.Println("Example Caddyfile:")
			fmt.Println("  your-domain.com {")
			fmt.Println("    reverse_proxy localhost:9201")
			fmt.Println("  }")
			fmt.Println()

			origin := promptString("What URL will users access? (e.g. https://agentique.example.com): ")
			if origin != "" {
				cfg.Server.RPOrigin = origin
			}
		}
	}

	// Step 4: Auth.
	if networkMode {
		fmt.Println()
		fmt.Println("Enable passkey authentication?")
		authChoice := promptChoice([]string{
			"Yes (recommended for network access)",
			"No (trusted network only)",
		}, 0)
		cfg.Server.DisableAuth = authChoice == 1
	}

	// Step 5: First project (optional).
	fmt.Println()
	projectPath := promptString("Path to your first project (leave empty to skip): ")
	if projectPath != "" {
		// Expand ~ if present.
		if strings.HasPrefix(projectPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				projectPath = filepath.Join(home, projectPath[2:])
			}
		}
		absPath, err := filepath.Abs(projectPath)
		if err == nil {
			projectPath = absPath
		}
		if info, err := os.Stat(projectPath); err != nil || !info.IsDir() {
			fmt.Printf("  Warning: %s is not a directory, skipping\n", projectPath)
		} else {
			cfg.Setup.InitialProject = projectPath
			fmt.Printf("  Project: %s (will be added on first start)\n", projectPath)
		}
	}

	// Step 6: Write config.
	configPath := config.Path()
	fmt.Println()
	fmt.Printf("Saving config to %s\n", configPath)
	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Println("Config saved.")

	// Step 7: Service install.
	fmt.Println()
	fmt.Println("Install as a system service? (auto-starts on login)")
	svcChoice := promptChoice([]string{
		"Yes",
		"No, I'll run it manually",
	}, 0)

	if svcChoice == 0 {
		fmt.Println()
		if err := service.Install(); err != nil {
			fmt.Fprintf(os.Stderr, "Service install failed: %v\n", err)
			fmt.Println("You can try again later with: agentique service install")
		} else {
			st, _ := service.GetStatus()
			fmt.Println("Service installed and started")
			if st.Running {
				fmt.Printf("  PID: %d\n", st.PID)
			}
		}
	}

	// Step 7: Summary.
	fmt.Println()
	fmt.Println("Setup complete")
	fmt.Println("==============")
	fmt.Printf("  Config: %s\n", configPath)
	fmt.Printf("  Data:   %s\n", paths.DataDir())
	fmt.Printf("  Addr:   %s\n", cfg.Server.Addr)

	if cfg.Server.TLSCert != "" {
		fmt.Println("  TLS:    enabled")
	}
	if cfg.Server.DisableAuth {
		fmt.Println("  Auth:   disabled")
	} else {
		fmt.Println("  Auth:   passkey (WebAuthn)")
	}

	if svcChoice != 0 {
		fmt.Println()
		fmt.Println("Start with: agentique serve")
	} else {
		scheme := "http"
		if cfg.Server.TLSCert != "" {
			scheme = "https"
		}
		host, port, _ := net.SplitHostPort(cfg.Server.Addr)
		if host == "" || host == "0.0.0.0" {
			host = "localhost"
		}
		fmt.Printf("\nOpen %s://%s:%s in your browser\n", scheme, host, port)
	}

	return nil
}

// promptChoice shows numbered options and returns the 0-based index.
func promptChoice(options []string, defaultIdx int) int {
	for i, opt := range options {
		marker := "  "
		if i == defaultIdx {
			marker = "* "
		}
		fmt.Printf("  %s[%d] %s\n", marker, i+1, opt)
	}
	for {
		fmt.Printf("Choice [%d]: ", defaultIdx+1)
		line, _ := setupReader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return defaultIdx
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(options) {
			fmt.Printf("Enter 1-%d\n", len(options))
			continue
		}
		return n - 1
	}
}

func promptString(prompt string) string {
	fmt.Print(prompt)
	line, _ := setupReader.ReadString('\n')
	return strings.TrimSpace(line)
}

func printChecks(checks []doctor.Check) {
	for _, c := range checks {
		icon := "\033[32m✓\033[0m"
		if c.Status == doctor.Warn {
			icon = "\033[33m!\033[0m"
		} else if c.Status == doctor.Fail {
			icon = "\033[31m✗\033[0m"
		}
		req := ""
		if !c.Required {
			req = " (optional)"
		}
		fmt.Printf("  %s  %-14s %s%s\n", icon, c.Name, c.Message, req)
		if c.Fix != "" && c.Status != doctor.OK {
			fmt.Printf("     %-14s %s\n", "", c.Fix)
		}
	}
}

func generateSelfSignedCert(certPath, keyPath string) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	certFile, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certFile.Close()
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}

	keyFile, err := os.OpenFile(keyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return nil
}
