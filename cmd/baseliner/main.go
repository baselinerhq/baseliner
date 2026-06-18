// Command baseliner scans repositories for baseline compliance.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/baselinerhq/baseliner/internal/introspect"
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
	root.AddCommand(newScanCmd(), newChecksCmd(), newPolicyCmd())
	return root
}

// newChecksCmd lists the built-in checks and their default severities.
func newChecksCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "checks",
		Short: "List the built-in checks and their default severities.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows, err := introspect.Catalog()
			if err != nil {
				return printErr(cmd, "%v", err)
			}
			switch format {
			case "json":
				return introspect.WriteJSON(cmd.OutOrStdout(), rows)
			case "table":
				introspect.WriteChecksTable(cmd.OutOrStdout(), rows)
				return nil
			default:
				return printErr(cmd, "invalid --format %q: must be table or json", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table or json.")
	return cmd
}

// newPolicyCmd prints the effective policy for a config (checks + ignores).
func newPolicyCmd() *cobra.Command {
	var format, configPath string
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Print the effective policy for a config (checks, severities, ignores).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			eff, err := introspect.Effective(configPath)
			if err != nil {
				return printErr(cmd, "%v", err)
			}
			switch format {
			case "json":
				return introspect.WriteJSON(cmd.OutOrStdout(), eff)
			case "table":
				introspect.WritePolicyTable(cmd.OutOrStdout(), eff)
				return nil
			default:
				return printErr(cmd, "invalid --format %q: must be table or json", format)
			}
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "baseliner.yaml", "Path to baseliner configuration file.")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table or json.")
	return cmd
}

// printErr writes the message to stderr and returns an error (the root silences
// cobra's own error output), so the command exits non-zero with a clean message.
func printErr(cmd *cobra.Command, format string, a ...any) error {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintln(cmd.ErrOrStderr(), msg)
	return errors.New(msg)
}

func newScanCmd() *cobra.Command {
	var (
		opts             runner.Options
		verbose, quietFl bool
		failUnder        float64
		publicContext    bool
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
			if cmd.Flags().Changed("public-context") {
				opts.PublicContext = &publicContext
			}
			exitCode = runner.Scan(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.ConfigPath, "config", "baseliner.yaml", "Path to baseliner configuration file.")
	f.StringVar(&opts.OutputFile, "output-file", "", "Write JSON output to this file.")
	f.StringVar(&opts.SarifFile, "sarif-file", "", "Also write SARIF 2.1.0 to this file (for GitHub code scanning).")
	f.StringVar(&opts.MarkdownFile, "markdown-file", "", "Also write a Markdown report to this file (for issues/PR comments).")
	f.StringVar(&opts.Format, "format", "both", "Output format: json, table, or both.")
	f.BoolVar(&opts.OpenIssues, "open-issues", false, "Open GitHub issues for findings.")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Skip all API write calls; log intent.")
	f.Float64Var(&failUnder, "fail-under", 0, "Exit 1 if any repo scores below this threshold (0.0–1.0); replaces the default per-check gate.")
	f.BoolVar(&publicContext, "public-context", false, "Treat output as public: protect private/internal repos per privacy.private_repos (default redact).")
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
