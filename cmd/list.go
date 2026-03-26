package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List known error issues and their pipeline stage",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		status, _ := cmd.Flags().GetString("status")
		service, _ := cmd.Flags().GetString("service")

		return runList(mgr, status, service, os.Stdout)
	},
}

func runList(mgr *reports.Manager, statusFilter, serviceFilter string, w io.Writer) error {
	issues, err := mgr.ListIssues(false)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ISSUE ID\tSTAGE")
	fmt.Fprintln(tw, "--------\t-----")

	for _, issue := range issues {
		if statusFilter != "" && string(issue.Stage) != statusFilter {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\n", issue.ID, issue.Stage)
	}

	return tw.Flush()
}

func init() {
	listCmd.Flags().String("status", "", "filter by stage: scanned, investigated, fixed")
	listCmd.Flags().String("service", "", "filter by service name")
	rootCmd.AddCommand(listCmd)
}
