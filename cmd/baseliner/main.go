// Command baseliner scans repositories for baseline compliance.
package main

import (
	"fmt"
	"os"

	"github.com/baselinerhq/baseliner/internal/policy"
	"github.com/baselinerhq/baseliner/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// Cobra already prints the error; exit 2 for runtime/config errors.
		os.Exit(2)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "baseliner",
		Short:         "Scan repositories for baseline compliance",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScanCmd())
	return root
}

// newScanCmd is the Phase 1 scan skeleton: flags + policy load are wired;
// discovery/collection/evaluation/output land in subsequent components.
func newScanCmd() *cobra.Command {
	var (
		configPath string
		outputFile string
		format     string
		openIssues bool
		dryRun     bool
		verbose    bool
		quiet      bool
	)
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan repositories defined by a baseliner config",
		RunE: func(cmd *cobra.Command, args []string) error {
			pol, err := policy.Load("default")
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"baseliner %s — loaded policy %q (%d checks)\n",
				version.Version, pol.ID, len(pol.Checks))
			fmt.Fprintln(cmd.OutOrStdout(),
				"scan pipeline not yet wired (Phase 1 in progress)")
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "baseliner.yaml", "path to config file")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "write JSON results to file")
	cmd.Flags().StringVar(&format, "format", "both", "output mode: json, table, or both")
	cmd.Flags().BoolVar(&openIssues, "open-issues", false, "open/update findings issue per GitHub repo")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "skip API write calls")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "debug logging")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress table output; keep errors")
	return cmd
}
