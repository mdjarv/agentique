package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	dbpkg "github.com/allbin/agentique/backend/db"
	"github.com/allbin/agentique/backend/internal/store"
)

func init() {
	authCmd.AddCommand(authResetCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
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
	db, err := store.Open("agentique.db")
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrations: %w", err)
	}
	return store.New(db), func() { db.Close() }, nil
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
