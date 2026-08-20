package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

// Multi-machine pairing (docs/multi-machine-research.md M0). `agentique pair`
// talks to the *running* server over HTTP, proving data-dir access with the
// admin secret the server persisted at startup — deliberately not a second
// writer to the live SQLite database.

var pairTTL time.Duration

func init() {
	pairCmd.Flags().DurationVar(&pairTTL, "ttl", 5*time.Minute, "pairing token lifetime")
	rootCmd.AddCommand(pairCmd)

	authCmd.AddCommand(authSessionsCmd)
	authCmd.AddCommand(authRevokeCmd)
}

var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Mint a one-time pairing token for connecting another device or machine",
	Long: `Mint a one-time, short-lived pairing token against the running server.

Paste the token (or the full pairing info) into the client you want to connect
— it is exchanged once for a long-lived bearer session. Manage or revoke
paired sessions later with "agentique auth sessions" and "agentique auth revoke".`,
	RunE: runPair,
}

var authSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List auth sessions (paired clients and web logins)",
	RunE:  runAuthSessions,
}

var authRevokeCmd = &cobra.Command{
	Use:   "revoke <session-id>",
	Short: "Revoke an auth session by id (see: agentique auth sessions)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuthRevoke,
}

// adminRequest performs an HTTP request against the local server, authorized
// by the data-dir admin secret.
func adminRequest(method, path string, body any) (*http.Response, error) {
	secretPath := filepath.Join(paths.DataDir(), "admin-secret")
	raw, err := os.ReadFile(secretPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no admin secret at %s — is the server running with auth enabled? (start with: agentique serve)", secretPath)
		}
		return nil, fmt.Errorf("read admin secret: %w", err)
	}

	req, err := newJSONRequest(method, baseURL()+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Agentique-Admin-Secret", strings.TrimSpace(string(raw)))

	resp, err := apiClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("server not reachable at %s — start it with: agentique serve (%w)", baseURL(), err)
	}
	return resp, nil
}

func runPair(cmd *cobra.Command, args []string) error {
	// Surface a clear error when auth is off — pairing is meaningless then.
	status, err := fetchJSON[map[string]any](apiClient(), baseURL()+"/api/auth/status")
	if err != nil {
		return fmt.Errorf("server not reachable at %s — start it with: agentique serve", baseURL())
	}
	if enabled, ok := status["authEnabled"].(bool); ok && !enabled {
		return fmt.Errorf("auth is disabled on this server — pairing requires auth (remove --disable-auth / [server] disable-auth)")
	}

	resp, err := adminRequest(http.MethodPost, "/api/auth/pairing-tokens",
		map[string]any{"ttlSeconds": int(pairTTL.Seconds())})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	minted, err := decodeJSONResponse[struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expiresAt"`
	}](resp)
	if err != nil {
		return fmt.Errorf("mint pairing token: %w", err)
	}

	env, envErr := fetchJSON[struct {
		MachineID string `json:"machineId"`
		Label     string `json:"label"`
	}](apiClient(), baseURL()+"/.well-known/agentique/environment")

	fmt.Println()
	if envErr == nil && env.Label != "" {
		fmt.Printf("  Machine:  %s (%s)\n", env.Label, shortID(env.MachineID))
	}
	fmt.Printf("  Server:   %s\n", baseURL())
	fmt.Printf("  Token:    %s\n", minted.Token)
	fmt.Printf("  Expires:  %s\n", minted.ExpiresAt)
	fmt.Println()
	fmt.Println("  In the client, add this machine using the server address and token above.")
	fmt.Println("  When pairing from another device, use an address it can reach")
	fmt.Println("  (e.g. this machine's tailnet name), not localhost.")
	fmt.Println()
	fmt.Println("  The token is single-use. Revoke paired clients later with:")
	fmt.Println("    agentique auth sessions")
	fmt.Println("    agentique auth revoke <session-id>")
	return nil
}

func runAuthSessions(cmd *cobra.Command, args []string) error {
	resp, err := adminRequest(http.MethodGet, "/api/auth/sessions", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sessions, err := decodeJSONResponse[[]struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Label       string `json:"label"`
		Kind        string `json:"kind"`
		ExpiresAt   string `json:"expiresAt"`
		CreatedAt   string `json:"createdAt"`
	}](resp)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No auth sessions.")
		return nil
	}

	fmt.Printf("  %-14s %-8s %-16s %-16s %s\n", "ID", "KIND", "USER", "LABEL", "EXPIRES")
	for _, s := range sessions {
		label := s.Label
		if label == "" {
			label = "-"
		}
		fmt.Printf("  %-14s %-8s %-16s %-16s %s\n", s.ID, s.Kind, s.DisplayName, label, s.ExpiresAt)
	}
	return nil
}

func runAuthRevoke(cmd *cobra.Command, args []string) error {
	resp, err := adminRequest(http.MethodDelete, "/api/auth/sessions/"+args[0], nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("revoke failed: %s", readErrorBody(resp))
	}
	fmt.Printf("Revoked session %s.\n", args[0])
	return nil
}
