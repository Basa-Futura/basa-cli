package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Basa-Futura/basa-cli/internal/config"
	"github.com/Basa-Futura/basa-cli/internal/fail"
	"github.com/Basa-Futura/basa-cli/internal/output"
	"github.com/Basa-Futura/basa-cli/internal/update"
)

// EnvVarNoUpdateCheck turns the background release check off.
const EnvVarNoUpdateCheck = "BASA_NO_UPDATE_CHECK"

// Seams for tests: where releases come from, and which file is "this binary".
// Production never changes either — releases live only on the upstream
// repository, and the binary to replace is the one running.
var (
	releasesURL = update.DefaultBaseURL
	executable  = func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		// Replace the real file, not a symlink pointing at it: renaming over the
		// link would leave the old binary where the link used to lead.
		return filepath.EvalSymlinks(exe)
	}
)

func releases(timeout time.Duration) *update.Releases {
	return &update.Releases{
		BaseURL:   releasesURL,
		HTTP:      &http.Client{Timeout: timeout},
		UserAgent: "basa-cli/" + Version,
	}
}

// updateResult is the JSON shape of `basa update`.
type updateResult struct {
	Previous string `json:"previous"`
	Latest   string `json:"latest"`
	Updated  bool   `json:"updated"`
}

func newUpdateCmd(out *output.Writer) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:         "update",
		Annotations: noConfig(),
		Short:       "Update basa to the latest release",
		Long: `Download the latest basa release and replace this one with it.

The download is checked against the release's published checksums before
anything is replaced, and the swap is atomic: if it fails partway, the basa
you already have is left exactly as it was.

basa also checks for a new release on its own, about once a day, and says so
on stderr when there is one. Set BASA_NO_UPDATE_CHECK=1 to turn that off.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd.Context(), out, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Install the latest release even if this build is already at it, or is a development build")
	return cmd
}

func runUpdate(ctx context.Context, out *output.Writer, force bool) error {
	asset, ok := update.Asset()
	if !ok {
		return fail.Usagef("There is no basa release for %s/%s.", runtime.GOOS, runtime.GOARCH)
	}

	// A development build is ahead of, behind, or beside the latest release,
	// and there is no telling which. Replacing it is what the operator asked
	// for only if they say so.
	current, isRelease := update.Parse(Version)
	previous := Version
	if isRelease {
		previous = current.String()
	} else if !force {
		return fail.UsageHintf(
			"Run: basa update --force to replace it with the latest release anyway.",
			"This is a development build (%s), not a release, so there is nothing to compare it with.", Version,
		)
	}

	exe, err := executable()
	if err != nil {
		return fail.Usagef("Could not find the basa binary to replace: %v", err)
	}

	rel := releases(2 * time.Minute)

	latest, err := rel.Latest(ctx)
	if errors.Is(err, update.ErrNoRelease) {
		return fail.Usage("No basa release has been published yet.")
	}
	if err != nil {
		return fail.UsageHintf("Check your network connection and try again.", "Could not check for a new version of basa: %v", err)
	}

	if isRelease && !force && !current.Less(latest) {
		return out.Data(updateResult{Previous: previous, Latest: latest.String()}, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "basa is up to date (%s).\n", current)
			return err
		})
	}

	out.Notice("Downloading basa %s for %s...", latest, asset[len("basa-"):])

	bin, err := rel.Download(ctx, latest, asset)
	if errors.Is(err, update.ErrChecksum) {
		return fail.UsageHintf(
			"Nothing was changed. Try again; if it happens twice, report it.",
			"The download of basa %s did not match its published checksum.", latest,
		)
	}
	if err != nil {
		return fail.UsageHintf("Nothing was changed. Check your network connection and try again.", "Could not download basa %s: %v", latest, err)
	}

	if err := update.Replace(exe, bin); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fail.UsageHintf(
				"Run basa update as the user who installed it, or re-run the installer.",
				"You do not have permission to replace %s.", exe,
			)
		}
		return fail.UsageHintf("Nothing was changed.", "Could not install basa %s: %v", latest, err)
	}

	return out.Data(updateResult{Previous: previous, Latest: latest.String(), Updated: true}, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "Updated basa from %s to %s.\n", previous, latest)
		return err
	})
}

// updateCheckCmd names the hidden command that does the checking, in a
// process of its own. See update.Checker for why it is a process.
const updateCheckCmd = "__update-check"

func newChecker(current update.Version) *update.Checker {
	return &update.Checker{
		Current:   current,
		Releases:  releases(10 * time.Second),
		StatePath: filepath.Join(config.Dir(), "update-check.json"),
	}
}

// startUpdateCheck is called as a command is about to run. It returns the
// checker whose saved answer the notice is read from after the command, or
// nil when this run should neither check nor say anything. If the saved
// answer is stale it also starts a check in the background, and does not wait.
func startUpdateCheck(cmd *cobra.Command, stderr io.Writer) *update.Checker {
	current, ok := update.Parse(Version)
	if !ok || !updateCheckWanted(cmd.Name(), os.Getenv, isTerminal(stderr)) {
		return nil
	}

	c := newChecker(current)
	if c.Due() {
		// Marked before the start, not after it succeeds: a check that cannot
		// even start should also wait out RetryAfter rather than be retried by
		// every command.
		c.MarkAttempt()
		_ = spawnCheck()
	}
	return c
}

// spawnCheck starts `basa __update-check` detached from this process: no
// stdio, its own process group, and never waited for. It is a variable so
// tests can see that a check was started without starting one.
var spawnCheck = func() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// #nosec G204 -- exe is this binary's own path, and the one argument is a
	// constant. Nothing here comes from input.
	child := exec.Command(exe, updateCheckCmd)
	child.Env = append(os.Environ(), EnvVarNoUpdateCheck+"=1")
	detach(child)
	if err := child.Start(); err != nil {
		return err
	}
	return child.Process.Release()
}

// newUpdateCheckCmd is the background half of the notice. It is hidden: it
// is started by basa, never typed, and prints nothing.
func newUpdateCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:         updateCheckCmd,
		Annotations: noConfig(),
		Hidden:      true,
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			current, ok := update.Parse(Version)
			if !ok {
				return nil
			}
			return newChecker(current).Refresh(cmd.Context())
		},
	}
}

// updateCheckWanted keeps every reason not to check in one place.
//
// The check exists to tell a person something, so it runs only when a person
// is reading stderr. A script, a pipe, or CI gets nothing: an unasked-for line
// in a log is noise, and an unasked-for network call from a build agent is
// worse. A development build never checks — see runUpdate for why it has
// nothing to compare against — and the caller has already ruled that out.
func updateCheckWanted(command string, getenv func(string) string, stderrIsTerminal bool) bool {
	switch command {
	case "update", updateCheckCmd, "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		// update reports on the same thing itself, and the check must not
		// start a check; completion output is read by a shell, not a person.
		return false
	}
	if getenv(EnvVarNoUpdateCheck) != "" || getenv("CI") != "" {
		return false
	}
	return stderrIsTerminal
}

// isTerminal is a variable so a test can stand in for a person at a terminal;
// a test's stderr is a buffer, which is never one.
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// printUpdateNotice says a newer release exists, if an earlier check found
// one. It reads the saved answer only, so it never waits on anything.
func printUpdateNotice(c *update.Checker, stderr io.Writer) {
	if c == nil {
		return
	}
	latest, ok := c.Newer()
	if !ok {
		return
	}
	fmt.Fprintf(stderr, "\nA new version of basa is available: %s (you have %s).\nRun: basa update\n", latest, c.Current)
}
