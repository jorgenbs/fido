// cmd/status.go
package cmd

import (
	"fmt"

	"github.com/ruter-as/fido/internal/pidfile"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the Fido daemon is running",
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := pidfile.Read(pidPath())
		if err != nil {
			fmt.Println("Fido is not running")
			return nil
		}

		if !pidfile.IsRunning(pid) {
			pidfile.Remove(pidPath())
			pidfile.Remove(portPath())
			fmt.Println("Fido is not running (cleaned up stale PID file)")
			return nil
		}

		port := "unknown"
		if p, err := pidfile.ReadPort(portPath()); err == nil {
			port = p
		}

		fmt.Printf("Fido is running (PID %d) on port %s\n", pid, port)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
