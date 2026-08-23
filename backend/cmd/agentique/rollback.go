package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/service"
	"github.com/mdjarv/agentique/backend/internal/update"
)

func init() {
	rollbackCmd.Flags().BoolVar(&rollbackNoRestart, "no-restart", false,
		"swap the binary back but leave the service alone")
	rootCmd.AddCommand(rollbackCmd)
}

var rollbackNoRestart bool

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Go back to the binary an upgrade replaced",
	Long: `Swap agentique.prev back over the installed binary and restart.

An in-app upgrade keeps the version it replaced as agentique.prev. Nothing
reverts automatically — an automatic rollback that also fails is a worse place
to be — so this is the deliberate way back.

A restart is not a pause: the service comes up and reaps orphaned CLI process
groups, so any turn running right now ends with it.`,
	RunE: runRollback,
}

func runRollback(cmd *cobra.Command, args []string) error {
	target, err := service.BinaryPath()
	if err != nil {
		return fmt.Errorf("resolve installed binary: %w", err)
	}

	if err := update.Rollback(target); err != nil {
		return err
	}
	fmt.Printf("Rolled back %s\n", target)

	if rollbackNoRestart {
		fmt.Println("Service left alone — restart it to run the restored binary.")
		return nil
	}

	st, serr := service.GetStatus()
	if serr != nil || !st.Installed {
		fmt.Fprintln(os.Stderr, "No service installed — restart agentique yourself to run the restored binary.")
		return nil
	}
	if err := service.Restart(); err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	fmt.Println("Service restarted.")
	return nil
}
