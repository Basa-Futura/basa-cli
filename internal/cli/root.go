// Package cli assembles the command tree and owns the single place where an
// error becomes a message on stderr plus a process exit code.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Basa-Futura/basa-cli/internal/commands"
	"github.com/Basa-Futura/basa-cli/internal/config"
	"github.com/Basa-Futura/basa-cli/internal/fail"
	"github.com/Basa-Futura/basa-cli/internal/licenses"
	"github.com/Basa-Futura/basa-cli/internal/output"
)

// Stamped at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Run builds the command tree, executes it, and returns the process exit code.
// Centralising the error handling here is what keeps every command free of
// exit-code and stderr concerns.
func Run(args []string, stdout, stderr io.Writer) int {
	var (
		asJSON   bool
		envFlag  string
		teamFlag string
	)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "Could not read your basa configuration:", err)
		return fail.CodeUsage
	}

	out := output.New(stdout, stderr, false)
	deps := &commands.Deps{Config: cfg, Out: out}

	root := &cobra.Command{
		Use:   "basa",
		Short: "Look up Basa projects, deals, and contracts from the terminal",
		Long: `basa is a command-line client for Basa.

It reads the same data you can see in the browser, as you, with the same
permissions. It can do nothing that you could not already do in the web app.

Every command needs to know which environment to talk to, and there is no
default: pass --env, or set BASA_ENV. That is deliberate — a tool that quietly
assumes production is one typo away from trouble.`,
		Example: `  basa auth login --env staging --url https://staging.basa.example
  basa me --env staging
  basa me --env staging --json`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// A bare `basa` should teach rather than fail.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fail.UsageHint(fmt.Sprintf("There is no %q command.", args[0]), "Run: basa --help")
		},
	}

	root.PersistentFlags().BoolVar(&asJSON, "json", false, "Output JSON instead of a table")
	root.PersistentFlags().StringVarP(&envFlag, "env", "e", "", "Which Basa environment to talk to (required)")
	root.PersistentFlags().StringVarP(&teamFlag, "team", "t", "", "Which team, by name or id (needed if you belong to several)")

	// Flags are parsed before any RunE fires, so fold them into the shared deps
	// at that point rather than threading them through every constructor.
	root.PersistentPreRun = func(_ *cobra.Command, _ []string) {
		out.JSON = asJSON
		deps.EnvFlag = envFlag
		deps.TeamFlag = teamFlag
	}

	root.AddCommand(
		commands.NewAuthCmd(deps),
		commands.NewMeCmd(deps),
		commands.NewDealsCmd(deps),
		commands.NewContractsCmd(deps),
		newVersionCmd(stdout),
		newLicensesCmd(stdout),
	)

	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)

	if err := root.ExecuteContext(context.Background()); err != nil {
		// Cobra validates positional arguments before PersistentPreRun fires, so
		// an Args error arrives here with out.JSON still at its default even
		// though --json was already parsed. Sync it from the flag, or JSON mode
		// gets an empty stdout for exactly the error a script most needs to
		// read.
		out.JSON = asJSON
		out.Error(fail.MessageOf(err), fail.HintOf(err))
		return fail.CodeOf(err)
	}

	return fail.CodeOK
}

// newLicensesCmd makes the embedded third-party notices reachable from the
// binary itself. THIRD-PARTY-NOTICES.md travels with the source; this travels
// with the download, which is where the MIT and BSD obligations actually bite.
func newLicensesCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "licenses",
		Short: "Print third-party licence notices",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return licenses.WriteTo(stdout)
		},
	}
}

func newVersionCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Fprintf(stdout, "basa %s (commit %s, built %s)\n", Version, Commit, Date)
		},
	}
}

// Main keeps cmd/basa/main.go trivial.
func Main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
