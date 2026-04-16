package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/store"
)

func init() {
	authCmd.AddCommand(authRekeyCmd)
	authCmd.AddCommand(authResetCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
}

var authRekeyCmd = &cobra.Command{
	Use:   "rekey",
	Short: "Clear credentials and sessions so users can re-register passkeys",
	Long: `Remove all WebAuthn credentials and auth sessions while keeping user accounts.

Use this after changing the server domain (e.g. migrating from Tailscale to a
public DNS name with Let's Encrypt). Passkeys are bound to the domain they were
created on, so existing credentials become unusable after a domain change.

After running this command, each user can re-register a passkey by visiting the
app and entering their display name.`,
	RunE: runAuthRekey,
}

var authResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Delete all users, credentials, and sessions",
	RunE:  runAuthReset,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show auth state (users, credentials, sessions)",
	RunE:  runAuthStatus,
}

func openDB() (*store.Queries, func(), error) {
	db, err := store.Open(resolveDBPath())
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrations: %w", err)
	}
	return store.New(db), func() { db.Close() }, nil
}

func runAuthRekey(cmd *cobra.Command, args []string) error {
	queries, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	users, err := queries.ListUsers(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("No users found — nothing to rekey.")
		return nil
	}

	fmt.Printf("Users (%d):\n", len(users))
	for _, u := range users {
		creds, err := queries.ListCredentialsByUser(ctx, u.ID)
		if err != nil {
			return err
		}
		admin := ""
		if u.IsAdmin != 0 {
			admin = " (admin)"
		}
		fmt.Printf("  %s  %s%s  %d credential(s)\n", shortID(u.ID), u.DisplayName, admin, len(creds))
	}
	fmt.Println()

	if err := queries.DeleteAllWebAuthnCredentials(ctx); err != nil {
		return fmt.Errorf("delete credentials: %w", err)
	}
	if err := queries.DeleteAllAuthSessions(ctx); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}

	fmt.Println("Cleared all credentials and sessions.")
	fmt.Println("Users can now re-register passkeys by visiting the app and entering their display name.")
	return nil
}

func runAuthReset(cmd *cobra.Command, args []string) error {
	queries, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	users, err := queries.ListUsers(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("No users to reset.")
		return nil
	}

	for _, u := range users {
		if err := queries.DeleteUser(ctx, u.ID); err != nil {
			return fmt.Errorf("delete user %s: %w", u.ID, err)
		}
	}

	fmt.Fprintf(os.Stdout, "Deleted %d user(s) and all associated credentials/sessions.\n", len(users))
	return nil
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	queries, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	users, err := queries.ListUsers(ctx)
	if err != nil {
		return err
	}

	if len(users) == 0 {
		fmt.Println("No registered users.")
		return nil
	}

	for _, u := range users {
		creds, err := queries.ListCredentialsByUser(ctx, u.ID)
		if err != nil {
			return err
		}
		admin := ""
		if u.IsAdmin != 0 {
			admin = " (admin)"
		}
		fmt.Printf("  %s  %s%s  %d credential(s)\n", shortID(u.ID), u.DisplayName, admin, len(creds))
	}

	return nil
}
