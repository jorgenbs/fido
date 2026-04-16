package cmd

import (
	"fmt"

	"github.com/jorgenbs/fido/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Fido version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("fido %s\n", version.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
