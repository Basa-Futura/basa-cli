package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Basa-Futura/basa-cli/internal/client"
	"github.com/Basa-Futura/basa-cli/internal/config"
	"github.com/Basa-Futura/basa-cli/internal/fail"
	"github.com/Basa-Futura/basa-cli/internal/output"
)

// NewAuthCmd builds the `basa auth` group.
func NewAuthCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log in, log out, and check who you are",
	}
	cmd.AddCommand(newAuthLoginCmd(deps), newAuthLogoutCmd(deps), newAuthStatusCmd(deps))
	return cmd
}

func newAuthLoginCmd(deps *Deps) *cobra.Command {
	var url string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a Basa API token for an environment",
		Long: `Store a Basa API token for an environment.

Get a token from Basa in your browser: the settings menu, then API Tokens.
Give it the "read" ability. It is shown once, so copy it before closing.

The token is never passed as an argument — it would land in your shell
history. Paste it at the prompt, or pipe it in.`,
		Example: `  basa auth login --env staging --url https://staging.basa.example
  echo "$TOKEN" | basa auth login --env staging`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthLogin(cmd.Context(), deps, url)
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "Base URL of the environment (required the first time)")

	return cmd
}

func runAuthLogin(ctx context.Context, deps *Deps, url string) error {
	cfg, out := deps.Config, deps.Out

	// The environment must be named explicitly. There is no default, so
	// `basa auth login` on its own is an error rather than a guess.
	env := deps.EnvFlag
	if env == "" {
		env = os.Getenv(config.EnvVarEnvironment)
	}
	if env == "" {
		return fail.UsageHint(
			"Which environment are you logging in to?",
			"Pass --env, for example: basa auth login --env staging --url https://staging.basa.example",
		)
	}

	// A known environment keeps its URL; a new one needs one.
	existing, known := cfg.Environments[env]
	switch {
	case url != "":
		cfg.SetEnvironment(env, url)
	case known:
		cfg.SetEnvironment(env, existing.URL)
	default:
		return fail.UsageHint(
			fmt.Sprintf("Environment %q is new, so basa needs its URL.", env),
			fmt.Sprintf("Run: basa auth login --env %s --url https://<host>", env),
		)
	}

	token, err := readToken(out)
	if err != nil {
		return err
	}
	if token == "" {
		return fail.Usage("No token was entered.")
	}

	// Prove the token works before storing it. Storing an unusable token means
	// the operator discovers the problem later, in a different command, with a
	// confusing message.
	me, err := client.New(env, cfg.Environments[env].URL, token).Me(ctx)
	if err != nil {
		return err
	}

	if err := cfg.SaveToken(env, token); err != nil {
		return fail.Wrap(fail.CodeUsage, "Could not save the token.", err)
	}
	if err := cfg.Save(); err != nil {
		return fail.Wrap(fail.CodeUsage, "Could not save the configuration.", err)
	}

	if w := cfg.FallbackWarning(); w != "" {
		out.Notice("Note: %s", w)
	}

	out.Notice("Logged in to %s as %s.", env, me.Email)

	return nil
}

// readToken takes the token from a pipe when stdin is not a terminal, and from
// a no-echo prompt when it is. Never from argv.
func readToken(out *output.Writer) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			return strings.TrimSpace(scanner.Text()), nil
		}
		return "", fail.UsageHint("No token was piped in.", "Pipe one: echo \"$TOKEN\" | basa auth login --env <name>")
	}

	out.Notice("Paste your Basa API token (it will not be shown):")

	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fail.Wrap(fail.CodeUsage, "Could not read the token.", err)
	}

	return strings.TrimSpace(string(raw)), nil
}

func newAuthLogoutCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored token for an environment",
		Long: `Remove the stored token for an environment.

This deletes the copy on this machine. It does not revoke the token on the
server — to do that, delete it in Basa under settings, then API Tokens. If you
think a token has been exposed, revoke it there.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, out := deps.Config, deps.Out

			env := deps.EnvFlag
			if env == "" {
				env = os.Getenv(config.EnvVarEnvironment)
			}
			if env == "" {
				return fail.UsageHint("Which environment are you logging out of?", "Pass --env, for example: basa auth logout --env staging")
			}

			if err := cfg.DeleteToken(env); err != nil {
				return fail.Wrap(fail.CodeUsage, "Could not remove the stored token.", err)
			}

			out.Notice("Removed the stored token for %s.", env)
			out.Notice("It stays valid on the server until it expires — revoke it in Basa if it may have been exposed.")

			return nil
		},
	}
}

func newAuthStatusCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show who you are and which environment is active",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMe(cmd.Context(), deps)
		},
	}
}
