package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Basa-Futura/basa-cli/internal/cli"
	"github.com/Basa-Futura/basa-cli/internal/config"
)

// fakeRelease stands in for github.com/Basa-Futura/basa-cli: v0.1.7 is the
// latest release, and it publishes this platform's binary and checksums.txt.
// GitHub is the only thing faked; the command, the download, the checksum
// check, and the swap are all real.
type fakeRelease struct {
	*httptest.Server
	downloads atomic.Int32
}

func newFakeRelease(t *testing.T, bin []byte, checksum string) *fakeRelease {
	t.Helper()
	asset := "basa-" + runtime.GOOS + "-" + runtime.GOARCH

	fr := &fakeRelease{}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v0.1.7", http.StatusFound)
	})
	mux.HandleFunc("/releases/download/v0.1.7/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksum + "  " + asset + "\n"))
	})
	mux.HandleFunc("/releases/download/v0.1.7/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		fr.downloads.Add(1)
		_, _ = w.Write(bin)
	})
	fr.Server = httptest.NewServer(mux)
	t.Cleanup(fr.Close)

	cli.SetReleasesURL(t, fr.URL)
	return fr
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// installedBinary is the "basa" that update replaces.
func installedBinary(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "basa")
	if err := os.WriteFile(exe, []byte("old basa"), 0o755); err != nil {
		t.Fatal(err)
	}
	cli.SetExecutable(t, exe)
	return exe
}

func contents(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runCLI(args ...string) (string, string, int) {
	var out, errBuf bytes.Buffer
	code := cli.Run(args, &out, &errBuf)
	return out.String(), errBuf.String(), code
}

func TestUpdateReplacesTheBinaryWithTheLatestRelease(t *testing.T) {
	newBin := []byte("new basa")
	newFakeRelease(t, newBin, sha(newBin))
	exe := installedBinary(t)
	cli.SetVersion(t, "0.1.6")

	stdout, stderr, code := runCLI("update")

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if want := "Updated basa from v0.1.6 to v0.1.7.\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if got := contents(t, exe); got != "new basa" {
		t.Errorf("binary = %q, want the downloaded release", got)
	}
}

func TestUpdateJSON(t *testing.T) {
	newBin := []byte("new basa")
	newFakeRelease(t, newBin, sha(newBin))
	installedBinary(t)
	cli.SetVersion(t, "0.1.6")

	stdout, stderr, code := runCLI("update", "--json")

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := map[string]any{"previous": "v0.1.6", "latest": "v0.1.7", "updated": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestUpdateWhenAlreadyCurrentDownloadsNothing(t *testing.T) {
	fr := newFakeRelease(t, []byte("new basa"), sha([]byte("new basa")))
	exe := installedBinary(t)
	cli.SetVersion(t, "0.1.7")

	stdout, stderr, code := runCLI("update")

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if want := "basa is up to date (v0.1.7).\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if n := fr.downloads.Load(); n != 0 {
		t.Errorf("downloaded the binary %d times, want 0", n)
	}
	if got := contents(t, exe); got != "old basa" {
		t.Errorf("binary = %q, want it untouched", got)
	}
}

// A download that does not match checksums.txt must never become the binary.
func TestUpdateRefusesAChecksumMismatch(t *testing.T) {
	newFakeRelease(t, []byte("tampered"), sha([]byte("new basa")))
	exe := installedBinary(t)
	cli.SetVersion(t, "0.1.6")

	stdout, stderr, code := runCLI("update")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on failure", stdout)
	}
	if want := "The download of basa v0.1.7 did not match its published checksum.\n"; !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
	if got := contents(t, exe); got != "old basa" {
		t.Errorf("binary = %q, want it untouched", got)
	}
}

// A development build has nothing to compare against, so it is replaced only
// when asked outright.
func TestUpdateOfADevelopmentBuildNeedsForce(t *testing.T) {
	newBin := []byte("new basa")
	fr := newFakeRelease(t, newBin, sha(newBin))
	exe := installedBinary(t)
	cli.SetVersion(t, "v0.1.6-3-gabc1234-dirty")

	_, stderr, code := runCLI("update")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := "This is a development build (v0.1.6-3-gabc1234-dirty), not a release, so there is nothing to compare it with.\n" +
		"Run: basa update --force to replace it with the latest release anyway.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if n := fr.downloads.Load(); n != 0 || contents(t, exe) != "old basa" {
		t.Errorf("a refused update downloaded %d times and left %q; want nothing touched", n, contents(t, exe))
	}

	stdout, stderr, code := runCLI("update", "--force")

	if code != 0 {
		t.Fatalf("--force: exit %d, stderr: %s", code, stderr)
	}
	if want := "Updated basa from v0.1.6-3-gabc1234-dirty to v0.1.7.\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if got := contents(t, exe); got != "new basa" {
		t.Errorf("binary = %q, want the downloaded release", got)
	}
}

// --- the notice ---

// noticeEnv is a person at a terminal on release v0.1.6, whose last check,
// checkedAgo ago, found v0.1.7. It returns how many background checks the
// test's commands start; none is ever really started, and a command never
// asks GitHub itself — the server fails the test if one does.
func noticeEnv(t *testing.T, checkedAgo time.Duration) (spawns *int, statePath string) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(cli.EnvVarNoUpdateCheck, "")
	t.Setenv("CI", "")
	cli.AssumeTerminal(t)
	cli.SetVersion(t, "0.1.6")
	spawns = cli.CountSpawns(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s: the command itself must never ask GitHub", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	cli.SetReleasesURL(t, srv.URL)

	state := `{"checked_at":"` + time.Now().Add(-checkedAgo).UTC().Format(time.RFC3339) + `","latest":"v0.1.7"}`
	statePath = filepath.Join(dir, "basa", "update-check.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	return spawns, statePath
}

const notice = "\nA new version of basa is available: v0.1.7 (you have v0.1.6).\nRun: basa update\n"

func TestANewerReleaseIsAnnouncedOnStderrAfterTheCommand(t *testing.T) {
	spawns, _ := noticeEnv(t, time.Hour)

	stdout, stderr, code := runCLI("version")

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := "basa 0.1.6 (commit none, built unknown)\n"; stdout != want {
		t.Errorf("stdout = %q, want only the version — the notice must never reach stdout", stdout)
	}
	if stderr != notice {
		t.Errorf("stderr = %q, want %q", stderr, notice)
	}
	if *spawns != 0 {
		t.Errorf("started %d background checks, want 0 an hour after the last one", *spawns)
	}
}

// A stale answer starts one background check, and is still what this run
// announces: the command does not wait for the new one.
func TestAStaleAnswerStartsOneBackgroundCheck(t *testing.T) {
	spawns, _ := noticeEnv(t, 25*time.Hour)

	_, stderr, _ := runCLI("version")

	if *spawns != 1 {
		t.Errorf("started %d background checks, want 1", *spawns)
	}
	if stderr != notice {
		t.Errorf("stderr = %q, want the saved answer announced, %q", stderr, notice)
	}

	// The next command, while that check is in flight or after it failed,
	// does not start another.
	runCLI("version")
	if *spawns != 1 {
		t.Errorf("after a second command, %d background checks started; want still 1", *spawns)
	}
}

func TestNoCheckStartsWhenStderrIsNotATerminal(t *testing.T) {
	spawns, _ := noticeEnv(t, 25*time.Hour)
	cli.SetTerminal(t, false)

	_, stderr, _ := runCLI("version")

	if stderr != "" || *spawns != 0 {
		t.Errorf("stderr = %q with %d checks started; want silence and none for a pipe", stderr, *spawns)
	}
}

// The hidden command the background check runs: it asks GitHub and saves the
// answer for the next command to announce.
func TestTheBackgroundCheckSavesTheLatestRelease(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cli.SetVersion(t, "0.1.6")
	newFakeRelease(t, nil, "")

	stdout, stderr, code := runCLI("__update-check")

	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want a silent success", code, stdout, stderr)
	}

	// What it saved is what the next command at a terminal announces.
	cli.AssumeTerminal(t)
	t.Setenv(cli.EnvVarNoUpdateCheck, "")
	t.Setenv("CI", "")
	spawns := cli.CountSpawns(t)
	_, stderr, _ = runCLI("version")
	if stderr != notice {
		t.Errorf("stderr = %q, want %q", stderr, notice)
	}
	if *spawns != 0 {
		t.Errorf("started %d checks straight after a successful one, want 0", *spawns)
	}
}

// After an error, the notice comes after the error's own hint, never between
// the message and the hint.
func TestTheNoticeFollowsAnError(t *testing.T) {
	noticeEnv(t, time.Hour)
	t.Setenv(config.EnvVarNoKeyring, "1")
	t.Setenv(config.EnvVarToken, "")
	t.Setenv(config.EnvVarEnvironment, "")

	_, stderr, code := runCLI("me")

	if code != 3 {
		t.Errorf("exit %d, want 3 (not logged in)", code)
	}
	want := "You are not logged in to \"production\".\nRun: basa auth login\n" + notice
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestTheNoticeCanBeTurnedOff(t *testing.T) {
	noticeEnv(t, time.Hour)
	t.Setenv(cli.EnvVarNoUpdateCheck, "1")

	_, stderr, _ := runCLI("version")

	if stderr != "" {
		t.Errorf("stderr = %q, want nothing with %s set", stderr, cli.EnvVarNoUpdateCheck)
	}
}

func TestUpdateCheckWanted(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	for _, tc := range []struct {
		name     string
		command  string
		env      map[string]string
		terminal bool
		want     bool
	}{
		{"a person at a terminal", "deals", nil, true, true},
		{"stderr piped or captured", "deals", nil, false, false},
		{"turned off", "deals", map[string]string{cli.EnvVarNoUpdateCheck: "1"}, true, false},
		{"CI", "deals", map[string]string{"CI": "true"}, true, false},
		{"update reports this itself", "update", nil, true, false},
		{"shell completion", "__complete", nil, true, false},
		{"completion script", "completion", nil, true, false},
	} {
		if got := cli.UpdateCheckWanted(tc.command, env(tc.env), tc.terminal); got != tc.want {
			t.Errorf("%s: UpdateCheckWanted = %v, want %v", tc.name, got, tc.want)
		}
	}
}
