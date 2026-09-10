package cli_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Basa-Futura/basa-cli/internal/config"
)

// Cobra validates positional arguments before PersistentPreRun fires, which is
// where --json was folded into the writer. So a stray argument — the error a
// script is likeliest to trip — arrived with JSON mode still off and stdout
// empty, breaking the promise that stdout is always parseable.
func TestJSONModeRendersAStrayArgumentErrorOnStdout(t *testing.T) {
	h := newHarness(t, okHandler)
	t.Setenv(config.EnvVarToken, "42|t")

	stdout, _, code := h.run("deals", "list", "extra", "--env", "local", "--json")

	if code == 0 {
		t.Fatal("a stray argument must not exit 0")
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not JSON in --json mode: %v\n%q", err, stdout)
	}
	if !strings.Contains(payload.Error.Message, "takes no arguments") {
		t.Errorf("unexpected message %q", payload.Error.Message)
	}
}

// A reply that is syntactically complete but shorter than the server declared
// is a dropped connection, not an answer. Decoding what arrived and reporting
// success is the failure: the operator sees a clean result that is really the
// first part of one.
func TestATruncatedReplyIsAnErrorNotAResult(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Declare more than is sent. Go's server closes the connection when the
		// handler returns short, and the client reads an unexpected EOF after a
		// body that, on its own, decodes fine.
		w.Header().Set("Content-Length", strconv.Itoa(len(meBody)+512))
		_, _ = w.Write([]byte(meBody))
	})
	t.Setenv(config.EnvVarToken, "42|t")

	_, stderr, code := h.run("me", "--env", "local")

	if code == 0 {
		t.Fatalf("a truncated reply must not exit 0; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "cut off") {
		t.Errorf("should say the reply was cut off, got:\n%s", stderr)
	}
}

// Two teams can share a name. Returning the first exact match would quietly
// show one team's deals under a flag that named both — the one thing the
// no-guessing rule exists to prevent. The ambiguity has to reach the operator
// with ids to choose from.
func TestTwoTeamsWithOneNameIsAmbiguousNotAGuess(t *testing.T) {
	const twins = `{"data":{"id":42,"name":"Dana Reed","email":"dana@example.test",
	  "teams":[{"id":7,"name":"Acme Agency","personal":false},{"id":8,"name":"Acme Agency","personal":false}],
	  "token":{"name":"basa-cli","abilities":["read"],"expires_at":null}}}`

	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/me") {
			_, _ = w.Write([]byte(twins))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	t.Setenv(config.EnvVarToken, "42|t")

	_, stderr, code := h.run("deals", "list", "--env", "local", "--team", "acme agency")
	if code == 0 {
		t.Fatal("an ambiguous team name must not exit 0")
	}
	for _, want := range []string{"more than one", "(7)", "(8)"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("should name both teams with ids, missing %q:\n%s", want, stderr)
		}
	}

	// An id is never ambiguous.
	if _, stderr, code := h.run("deals", "list", "--env", "local", "--team", "8"); code != 0 {
		t.Fatalf("--team 8 should resolve, got exit %d:\n%s", code, stderr)
	}
}

// "Removed" has to mean removed. logout converted every deletion error into
// success, so a credentials file that could not be rewritten left the token on
// disk while the operator was told it was gone. Absent stays success — logout
// is idempotent — but only absent.
func TestLogoutReportsWhenTheTokenCouldNotBeRemoved(t *testing.T) {
	h := newHarness(t, okHandler)

	// The file backend keeps credentials.json in the config dir. Making that
	// path a directory fails every read of it, whatever the privileges.
	credPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "basa", "credentials.json")
	if err := os.MkdirAll(credPath, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, stderr, code := h.run("auth", "logout", "--env", "local")

	if code == 0 {
		t.Fatal("logout must not claim success when the token could not be removed")
	}
	if !strings.Contains(stderr, "Could not remove") {
		t.Errorf("should say it could not remove the token, got:\n%s", stderr)
	}
}

// corruptConfig replaces the harness's config.json with something that is not
// JSON, so every command that loads configuration fails at that step.
func corruptConfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "basa", "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// Configuration used to be loaded before Cobra dispatched anything, so a
// malformed config.json took down `basa --help`, `basa version`, and — the one
// with a legal job — `basa licenses`, none of which read it. Loading happens
// only for commands that need it now.
func TestCommandsThatNeedNoConfigSurviveABrokenOne(t *testing.T) {
	h := newHarness(t, okHandler)
	corruptConfig(t)

	for _, args := range [][]string{{"--help"}, {"version"}, {"licenses"}} {
		stdout, stderr, code := h.run(args...)
		if code != 0 {
			t.Errorf("basa %s: exit %d with a broken config:\n%s", strings.Join(args, " "), code, stderr)
		}
		if stdout == "" {
			t.Errorf("basa %s: printed nothing", strings.Join(args, " "))
		}
	}
}

// A command that does need configuration still fails clearly — this guards
// against over-correcting into swallowing the error.
func TestACommandThatNeedsConfigStillReportsABrokenOne(t *testing.T) {
	h := newHarness(t, okHandler)
	corruptConfig(t)
	t.Setenv(config.EnvVarToken, "42|t")

	_, stderr, code := h.run("me", "--env", "local")

	if code == 0 {
		t.Fatal("a broken config must not let a config-dependent command exit 0")
	}
	if !strings.Contains(stderr, "configuration") {
		t.Errorf("should name the configuration problem, got:\n%s", stderr)
	}
}
