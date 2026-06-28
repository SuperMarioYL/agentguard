// Command agentguard scans dependency trees for prose channels (README,
// CHANGELOG, docstrings, package metadata) that contain imperative text
// addressed to a coding agent — the supply-chain prompt-injection class
// demonstrated by the jqwik incident (May 2026).
//
// Typical use:
//
//	agentguard check .                       # scan cwd, text output
//	agentguard check . --format sarif        # SARIF 2.1.0 for CI upload
//	agentguard check . --changed-only lock   # diff against a baseline
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/agentguard/internal/detect"
	"github.com/SuperMarioYL/agentguard/internal/report"
	"github.com/SuperMarioYL/agentguard/internal/scan"
)

// version is overridden via -ldflags "-X main.version=v0.1.0" at release.
var version = "dev"

type checkOptions struct {
	format        string
	severityMin   string
	changedOnly   string
	writeBaseline string
	output        string
	noColor       bool
	ecosystems    []string
	exitOnFinding bool
}

func main() {
	if err := newRootCmd(os.Stdout, os.Stderr).Execute(); err != nil {
		// Cobra already printed the error; only set exit code.
		os.Exit(2)
	}
}

func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "agentguard",
		Short: "Catch hidden agent-directed imperatives in your dependencies",
		Long: "agentguard scans the prose channels of your installed packages " +
			"(README, CHANGELOG, docstrings, package metadata) for imperative " +
			"text addressed to a coding agent — the supply-chain prompt-" +
			"injection class made famous by the jqwik incident.\n\n" +
			"It is offline, stdlib-first, and ships as a single static binary.",
		Version:       resolveVersion(),
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.AddCommand(newCheckCmd(stdout, stderr))
	root.AddCommand(newCorpusCmd(stdout))
	return root
}

func newCheckCmd(stdout, stderr io.Writer) *cobra.Command {
	opts := &checkOptions{
		format:        "text",
		severityMin:   "medium",
		exitOnFinding: true,
	}

	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Scan a project directory for agent-targeted prompt-injection payloads",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			return runCheck(stdout, stderr, root, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.format, "format", "f", opts.format,
		"output format: text | sarif")
	cmd.Flags().StringVarP(&opts.severityMin, "severity", "s", opts.severityMin,
		"minimum severity to report and to gate the exit code: low | medium | high")
	cmd.Flags().StringVar(&opts.changedOnly, "changed-only", "",
		"path to a baseline JSON (from --write-baseline); skip prose files whose content hash is unchanged")
	cmd.Flags().StringVar(&opts.writeBaseline, "write-baseline", "",
		"after scanning, write a baseline JSON of all scanned prose hashes to this path (for a later --changed-only run)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "",
		"write the report to this file (default: stdout)")
	cmd.Flags().BoolVar(&opts.noColor, "no-color", false,
		"disable ANSI colors in text output")
	cmd.Flags().StringSliceVar(&opts.ecosystems, "ecosystem", nil,
		"restrict scan to these ecosystems: node | python | go (default: all detected)")
	cmd.Flags().BoolVar(&opts.exitOnFinding, "exit-on-finding", opts.exitOnFinding,
		"exit non-zero when any finding ≥ --severity is reported (CI mode)")

	return cmd
}

func newCorpusCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "corpus",
		Short: "Print the embedded payload corpus version and rule count",
		RunE: func(cmd *cobra.Command, args []string) error {
			meta, err := detect.CorpusMetadata()
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "corpus version: %s\n", meta.Version)
			fmt.Fprintf(stdout, "rules:          %d\n", meta.RuleCount)
			fmt.Fprintf(stdout, "last updated:   %s\n", meta.Updated)
			return nil
		},
	}
}

func runCheck(stdout, stderr io.Writer, root string, opts *checkOptions) error {
	if opts.noColor {
		color.NoColor = true
	}

	minSev, err := detect.ParseSeverity(opts.severityMin)
	if err != nil {
		return fmt.Errorf("--severity: %w", err)
	}

	detector, err := detect.NewDefault()
	if err != nil {
		return fmt.Errorf("load corpus: %w", err)
	}

	// Walk returns the FULL file set (ChangedOnly is not set on the walk)
	// so the baseline can cover every package.  The scan is narrowed
	// separately: filter against the PRE-EXISTING baseline first, then write
	// the new baseline from the full set, then scan the narrowed set.
	// Writing the baseline from the full set — not the narrowed set — is what
	// keeps the rolling-baseline pattern `--changed-only X --write-baseline X`
	// stable; otherwise the baseline collapses to just the changed packages
	// and the next run re-scans every previously-unchanged package.
	scanOpts := scan.Options{
		Root:       root,
		Ecosystems: opts.ecosystems,
	}
	files, err := scan.Walk(scanOpts)
	if err != nil {
		return fmt.Errorf("walk %q: %w", root, err)
	}

	scanFiles := files
	if opts.changedOnly != "" {
		scanFiles, err = scan.FilterChanged(files, opts.changedOnly)
		if err != nil {
			return fmt.Errorf("--changed-only: %w", err)
		}
	}

	if opts.writeBaseline != "" {
		data, err := scan.BaselineBytes(files)
		if err != nil {
			return fmt.Errorf("baseline: %w", err)
		}
		if err := os.WriteFile(opts.writeBaseline, data, 0o644); err != nil {
			return fmt.Errorf("write baseline %q: %w", opts.writeBaseline, err)
		}
	}

	findings, err := detector.ScanAll(scanFiles)
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}
	findings = detect.FilterMinSeverity(findings, minSev)

	out, closer, err := openOutput(opts.output, stdout)
	if err != nil {
		return err
	}
	defer closer()

	switch opts.format {
	case "text":
		if err := report.RenderText(out, findings); err != nil {
			return err
		}
	case "sarif":
		if err := report.RenderSARIF(out, findings, resolveVersion()); err != nil {
			return err
		}
	default:
		return fmt.Errorf("--format: unknown value %q (want text or sarif)", opts.format)
	}

	if opts.exitOnFinding && len(findings) > 0 {
		// Communicate the gate failure on stderr so piped reports stay clean.
		fmt.Fprintf(stderr, "agentguard: %d finding(s) at or above %s\n",
			len(findings), opts.severityMin)
		os.Exit(1)
	}
	return nil
}

func openOutput(path string, fallback io.Writer) (io.Writer, func(), error) {
	if path == "" {
		return fallback, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open output %q: %w", path, err)
	}
	return f, func() { _ = f.Close() }, nil
}

func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
