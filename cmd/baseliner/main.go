// Command baseliner scans repositories for baseline compliance.
package main

import (
	"log/slog"
	"os"

	"github.com/baselinerhq/baseliner/internal/runner"
	"github.com/baselinerhq/baseliner/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	os.Exit(run())
}

// run executes the CLI and returns the process exit code.
func run() int {
	if err := newRootCmd().Execute(); err != nil {
		// Cobra prints flag/usage errors; treat them as runtime errors.
		return 2
	}
	return exitCode
}

// exitCode carries the scan result out of the cobra RunE (which only returns error).
var exitCode int

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "baseliner",
		Short:         "Repository fleet baseline compliance engine.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n") // bare version, matching the Python CLI
	root.AddCommand(newScanCmd())
	return root
}

func newScanCmd() *cobra.Command {
	var (
		opts             runner.Options
		verbose, quietFl bool
		failUnder        float64
	)
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan a collection of repositories against the baseline policy.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			configureLogging(verbose, quietFl)
			opts.Quiet = quietFl
			if cmd.Flags().Changed("fail-under") {
				opts.FailUnder = &failUnder
			}
			exitCode = runner.Scan(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.ConfigPath, "config", "baseliner.yaml", "Path to baseliner configuration file.")
	f.StringVar(&opts.OutputFile, "output-file", "", "Write JSON output to this file.")
	f.StringVar(&opts.Format, "format", "both", "Output format: json, table, or both.")
	f.BoolVar(&opts.OpenIssues, "open-issues", false, "Open GitHub issues for findings.")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Skip all API write calls; log intent.")
	f.Float64Var(&failUnder, "fail-under", 0, "Exit 1 if any repo scores below this threshold (0.0–1.0); replaces the default per-check gate.")
	f.BoolVar(&verbose, "verbose", false, "Enable debug logging.")
	f.BoolVar(&quietFl, "quiet", false, "Suppress table output; keep errors.")
	return cmd
}

// configureLogging mirrors the Python levels: verbose=DEBUG, quiet=WARNING,
// default=INFO; verbose wins when both are set.
func configureLogging(verbose, quiet bool) {
	level := slog.LevelInfo
	switch {
	case verbose:
		level = slog.LevelDebug
	case quiet:
		level = slog.LevelWarn
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if verbose && quiet {
		slog.Debug("Both --verbose and --quiet given; --verbose wins")
	}
}
