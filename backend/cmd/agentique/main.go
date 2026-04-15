package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/service"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	addr    string
)

var rootCmd = &cobra.Command{
	Use:     "agentique",
	Short:   "Agentique — manage concurrent Claude Code agents",
	Version: version,
	RunE:    runStatus,
}

func init() {
	rootCmd.SetVersionTemplate("agentique {{.Version}}\n")
	rootCmd.PersistentFlags().StringVar(&addr, "addr", "localhost:9201", "server address")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runStatus shows server connection info and active session summary.
func runStatus(cmd *cobra.Command, args []string) error {
	fmt.Printf("agentique %s\n\n", version)

	// Load config to show resolved settings.
	cfg, _ := config.Load(config.Path())
	resolvedAddr := addr
	if !cmd.Flags().Changed("addr") && cfg.Server.Addr != "" {
		resolvedAddr = cfg.Server.Addr
	}

	tlsEnabled := cfg.Server.TLSCert != "" && cfg.Server.TLSKey != ""
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	authEnabled := !cfg.Server.DisableAuth
	host, port, _ := net.SplitHostPort(resolvedAddr)
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	url := fmt.Sprintf("%s://%s:%s", scheme, host, port)

	// Service status.
	st, err := service.GetStatus()
	if err == nil && st.Installed {
		if st.Running {
			fmt.Printf("  Service:  running (PID %d)\n", st.PID)
		} else {
			fmt.Printf("  Service:  installed, not running\n")
		}
	} else {
		fmt.Printf("  Service:  not installed\n")
	}

	fmt.Printf("  Address:  %s\n", resolvedAddr)
	fmt.Printf("  URL:      %s\n", url)
	fmt.Printf("  TLS:      %v\n", tlsEnabled)
	fmt.Printf("  Auth:     %v\n", authEnabled)

	// Override addr for API calls below.
	if !strings.Contains(resolvedAddr, "://") {
		resolvedAddr = scheme + "://" + resolvedAddr
	}
	base := strings.TrimRight(resolvedAddr, "/")

	// Health check.
	client := &http.Client{Timeout: 2 * time.Second}
	_, err = client.Get(base + "/api/health")
	if err != nil {
		fmt.Printf("  Health:   unreachable\n")
		fmt.Fprintf(os.Stderr, "\nServer not responding at %s\n", url)
		fmt.Fprintf(os.Stderr, "Start with: agentique serve\n")
		return nil
	}
	fmt.Printf("  Health:   ok\n")

	// Fetch projects + sessions summary.
	projects, err := fetchJSON[[]projectBrief](client, base+"/api/projects")
	if err != nil {
		return nil
	}

	projectByID := make(map[string]projectBrief)
	for _, p := range projects {
		projectByID[p.ID] = p
	}

	sessions, err := fetchJSON[[]sessionBrief](client, base+"/api/sessions")
	if err != nil {
		return nil
	}

	fmt.Println()
	if len(sessions) == 0 {
		fmt.Println("  No sessions")
		return nil
	}

	byProject := make(map[string][]sessionBrief)
	for _, s := range sessions {
		byProject[s.ProjectID] = append(byProject[s.ProjectID], s)
	}

	for projectID, ss := range byProject {
		p := projectByID[projectID]
		name := p.Name
		if name == "" {
			name = projectID[:8]
		}
		slug := p.Slug
		if slug == "" {
			slug = "-"
		}

		stateCounts := make(map[string]int)
		for _, s := range ss {
			stateCounts[s.State]++
		}

		parts := make([]string, 0)
		for _, state := range []string{"running", "idle", "done", "failed", "stopped"} {
			if n := stateCounts[state]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, state))
			}
		}

		label := "sessions"
		if len(ss) == 1 {
			label = "session"
		}

		fmt.Printf("  %-16s %-12s %d %s (%s)\n", name, slug, len(ss), label, strings.Join(parts, ", "))
	}

	return nil
}

func baseURL() string {
	a := addr
	if !strings.Contains(a, "://") {
		a = "http://" + a
	}
	return strings.TrimRight(a, "/")
}

func fetchJSON[T any](client *http.Client, url string) (T, error) {
	var zero T
	resp, err := client.Get(url)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, err
	}
	return result, nil
}

func postJSON(client *http.Client, url string, body any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	resp, err := client.Post(url, "application/json", &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func doRequest(client *http.Client, method, url string) error {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func resolveSession(client *http.Client, prefix string) (sessionBrief, error) {
	sessions, err := fetchJSON[[]sessionBrief](client, baseURL()+"/api/sessions")
	if err != nil {
		return sessionBrief{}, fmt.Errorf("failed to fetch sessions: %w", err)
	}

	var matches []sessionBrief
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, prefix) {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return sessionBrief{}, fmt.Errorf("no session matching %q", prefix)
	case 1:
		return matches[0], nil
	default:
		lines := make([]string, len(matches))
		for i, m := range matches {
			lines[i] = fmt.Sprintf("  %s  %s", m.ID[:8], m.Name)
		}
		return sessionBrief{}, fmt.Errorf("ambiguous prefix %q matches %d sessions:\n%s",
			prefix, len(matches), strings.Join(lines, "\n"))
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
