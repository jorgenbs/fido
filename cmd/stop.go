// cmd/stop.go
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/ruter-as/fido/internal/pidfile"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running Fido daemon",
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

		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("finding process %d: %w", pid, err)
		}

		fmt.Printf("Stopping Fido (PID %d)... ", pid)
		if err := proc.Signal(os.Interrupt); err != nil {
			return fmt.Errorf("sending signal: %w", err)
		}

		deadline := time.After(10 * time.Second)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-deadline:
				proc.Kill()
				fmt.Println("force killed")
				pidfile.Remove(pidPath())
				pidfile.Remove(portPath())
				return nil
			case <-ticker.C:
				if !pidfile.IsRunning(pid) {
					fmt.Println("done")
					pidfile.Remove(pidPath())
					pidfile.Remove(portPath())
					return nil
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
