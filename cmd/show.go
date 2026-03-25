package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruter-as/fido/internal/reports"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <issue-id>",
	Short: "Show reports for an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
		home, _ := os.UserHomeDir()
		reportsDir := filepath.Join(home, ".fido", "reports")
		mgr := reports.NewManager(reportsDir)

		return runShow(issueID, mgr)
	},
}

func runShow(issueID string, mgr *reports.Manager) error {
	if !mgr.Exists(issueID) {
		return fmt.Errorf("no reports found for issue %s", issueID)
	}

	stage := mgr.Stage(issueID)
	fmt.Printf("=== Issue: %s (stage: %s) ===\n\n", issueID, stage)

	if content, err := mgr.ReadError(issueID); err == nil {
		fmt.Println("--- error.md ---")
		fmt.Println(content)
		fmt.Println()
	}

	if content, err := mgr.ReadInvestigation(issueID); err == nil {
		fmt.Println("--- investigation.md ---")
		fmt.Println(content)
		fmt.Println()
	}

	if content, err := mgr.ReadFix(issueID); err == nil {
		fmt.Println("--- fix.md ---")
		fmt.Println(content)
		fmt.Println()
	}

	if resolve, err := mgr.ReadResolve(issueID); err == nil {
		fmt.Println("--- resolve.json ---")
		fmt.Printf("Branch:    %s\n", resolve.Branch)
		fmt.Printf("MR URL:    %s\n", resolve.MRURL)
		fmt.Printf("MR Status: %s\n", resolve.MRStatus)
		fmt.Printf("Service:   %s\n", resolve.Service)
		fmt.Printf("Created:   %s\n", resolve.CreatedAt)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(showCmd)
}
