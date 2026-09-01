package commands

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
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
	var (
		url       string
		noBrowser bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a Basa API token for an environment",
		Long: `Store a Basa API token for an environment.

You approve this CLI in your browser, and that is what mints the token. basa
prints the approval URL, and opens it for you when you are at a terminal. The
screen shows the token once, with a copy button.

The screen decides what the token may do — reading, the same things you can
already see in Basa — so there is nothing to pick and nothing to get wrong.

The token is never passed as an argument — it would land in your shell
history. Paste it at the prompt, or pipe it in.`,
		Example: `  basa auth login --env staging --url https://staging.basa.example
  basa auth login --env staging --no-browser
  echo "$TOKEN" | basa auth login --env staging`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthLogin(cmd.Context(), deps, url, noBrowser)
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "Base URL of the environment (required the first time)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open a browser (the approval URL is printed either way)")

	return cmd
}

func runAuthLogin(ctx context.Context, deps *Deps, url string, noBrowser bool) error {
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

	// Send the operator to the consent screen before asking for a token. Someone
	// running this for the first time does not have one yet, and the URL is the
	// only part of the flow they cannot work out for themselves.
	pair := pairURL(cfg.Environments[env].URL)
	out.Notice("Approve this CLI in your browser:\n\n    %s\n", pair)

	// Opening is a convenience; the printed URL is the part that always works.
	// Auto-open fails silently on headless boxes, over SSH, and in containers,
	// and in CI it would be wrong even if it succeeded — so it is gated on the
	// same interactivity check that decides whether to prompt at all.
	if shouldOpenBrowser(stdinIsTerminal(), noBrowser) {
		if !isBrowsable(pair) {
			out.Notice("Not opening that automatically — only http and https URLs are. Use the URL above.")
		} else if err := openBrowser(pair); err != nil {
			out.Notice("Could not open a browser — use the URL above.")
		}
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
	if !stdinIsTerminal() {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			return normalizeToken(scanner.Text()), nil
		}
		return "", fail.UsageHint("No token was piped in.", "Pipe one: echo \"$TOKEN\" | basa auth login --env <name>")
	}

	out.Notice("Paste your Basa API token (it will not be shown):")

	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fail.Wrap(fail.CodeUsage, "Could not read the token.", err)
	}

	return normalizeToken(string(raw)), nil
}

// normalizeToken is the only processing a pasted token gets, and it exists as a
// function so that both ways in — the pipe and the no-echo prompt — cannot drift
// apart on the one rule that matters.
//
// A Sanctum token is {id}|{40 alphanumerics}. The pipe is part of the
// credential, not a separator to be helpful about: splitting on it, or stripping
// anything between the ends, yields a string the server never issued and a 401
// the operator cannot explain. Surrounding whitespace is trimmed because a
// paste or an `echo` brings a newline with it — CRLF included, which is what a
// Windows clipboard delivers.
func normalizeToken(raw string) string {
	return strings.TrimSpace(raw)
}

// pairURL is the browser consent screen for an environment. The path is fixed
// by the server (ADR-053) and only the host varies, so this is the whole of the
// CLI's knowledge of that flow: there is no callback, no code to exchange, and
// nothing here to talk to over HTTP.
//
// No ?scope= is sent. The parameter exists, but an absent scope means "no
// preference" and the server then grants its own vocabulary — which is the
// answer we want, and one fewer thing to keep in step with it.
func pairURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/cli/pair"
}

// stdinIsTerminal is the one interactivity test in this package. The prompt and
// the browser open must agree about it: a piped login is automation, and
// automation gets neither.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// shouldOpenBrowser keeps both reasons not to open in one place.
func shouldOpenBrowser(interactive, noBrowser bool) bool {
	return interactive && !noBrowser
}

// isBrowsable reports whether a URL may be handed to the platform's opener.
// Only http and https, and only with a host.
//
// The openers are general-purpose "act on this thing" commands, not browsers:
// `open` on macOS resolves a filesystem path or launches whatever application
// has registered a URI scheme, and rundll32's FileProtocolHandler is no
// narrower. Handing one an unvalidated string turns a mistyped --url into
// "basa launched something".
//
// This is defence in depth rather than a hole being closed — the base URL is
// the operator's own, from --url or their config file, not anything a server
// sends. But a typo should not be able to invoke a URI handler, and refusing
// costs nothing: the URL has already been printed, which is the path that
// works in every case this rejects.
func isBrowsable(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}

	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// openBrowser hands the URL to the platform's opener. Three lines of exec beat
// a dependency here.
//
// Start rather than Run: the caller should not wait for a browser to exit, and
// on Linux xdg-open may not. That means the browser's own exit status is never
// seen — only whether the opener could be launched at all, which is exactly the
// headless case worth reporting. Either way the URL is already printed, so this
// failing costs the operator nothing.
//
// The URL comes from the operator's own configured environment, and is passed as
// an argument rather than through a shell, so there is nothing here to inject
// into.
func openBrowser(target string) error {
	var name string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		name, args = "xdg-open", []string{target}
	}

	return exec.Command(name, args...).Start()
}

func newAuthLogoutCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored token for an environment",
		Long: `Remove the stored token for an environment.

This deletes the copy on this machine. It does not revoke the token on the
server — to do that, delete it in Basa under settings, then API Tokens. A token
minted by approving this CLI is named "Basa CLI (paired)", which is the row to
look for. If you think a token has been exposed, revoke it there.`,
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
