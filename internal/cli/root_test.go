package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Basa-Futura/basa-cli/internal/cli"
	"github.com/Basa-Futura/basa-cli/internal/config"
)

const meBody = `{"data":{"id":42,"name":"Dana Reed","email":"dana@example.test",
  "teams":[{"id":7,"name":"Acme Agency","personal":false}],
  "token":{"name":"basa-cli","abilities":["read"],"expires_at":null}}}`

// harness stands up a fake Basa API and a throwaway config dir, then returns a
// run() that invokes the real command tree against them.
//
// The API is the only thing mocked — it is genuinely external to this binary.
// Everything under test (flag parsing, config, credentials, the HTTP client,
// rendering, exit codes) is the real implementation.
type harness struct {
	t      *testing.T
	server *httptest.Server
	run    func(args ...string) (stdout, stderr string, code int)
}

func newHarness(t *testing.T, handler http.HandlerFunc) *harness {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(config.EnvVarNoKeyring, "1")
	t.Setenv(config.EnvVarToken, "")
	t.Setenv(config.EnvVarEnvironment, "")

	// Pre-register the environment so tests can exercise commands without
	// going through login every time.
	cfgDir := filepath.Join(dir, "basa")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"environments":{"local":{"url":"` + srv.URL + `"}}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	h := &harness{t: t, server: srv}
	h.run = func(args ...string) (string, string, int) {
		var out, errBuf bytes.Buffer
		code := cli.Run(args, &out, &errBuf)
		return out.String(), errBuf.String(), code
	}
	return h
}

func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(meBody))
}

func status(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

// statusWithHeaders is status() for the cases where the header, not the body,
// is the thing under test — Retry-After being the one that exists today.
func statusWithHeaders(code int, headers map[string]string, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

// --- happy path ------------------------------------------------------------

func TestMeRendersATableForAHuman(t *testing.T) {
	h := newHarness(t, okHandler)
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, _, code := h.run("me", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	for _, want := range []string{"Environment", "local", "Dana Reed", "dana@example.test", "Acme Agency", "read"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table is missing %q\n--- stdout ---\n%s", want, stdout)
		}
	}
}

func TestMeEmitsTheAPIShapeForJSON(t *testing.T) {
	h := newHarness(t, okHandler)
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, _, code := h.run("me", "--env", "local", "--json")

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}

	var payload struct {
		Data struct {
			ID    int64  `json:"id"`
			Email string `json:"email"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if payload.Data.ID != 42 || payload.Data.Email != "dana@example.test" {
		t.Fatalf("unexpected payload: %+v", payload.Data)
	}
}

// Whatever else changes, stdout must stay machine-readable. A diagnostic
// leaking into it breaks every `basa ... --json | jq` in a script.
func TestJSONModeKeepsDiagnosticsOffStdout(t *testing.T) {
	h := newHarness(t, okHandler)
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, _, _ := h.run("me", "--env", "local", "--json")

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not pure JSON:\n%s", stdout)
	}
}

// --- failure paths ---------------------------------------------------------

func TestRejectedTokenExitsThreeWithAnActionableMessage(t *testing.T) {
	h := newHarness(t, status(http.StatusUnauthorized, `{"message":"Unauthenticated."}`))
	t.Setenv(config.EnvVarToken, "42|expired")

	stdout, stderr, code := h.run("me", "--env", "local")

	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	if !strings.Contains(stderr, "basa auth login") {
		t.Errorf("stderr should tell the operator how to fix it, got:\n%s", stderr)
	}
	if strings.Contains(stderr, "goroutine") || strings.Contains(stderr, ".go:") {
		t.Errorf("stderr looks like a stack trace:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on failure without --json, got:\n%s", stdout)
	}
}

func TestForbiddenExitsFourAndPrefersTheServersOwnMessage(t *testing.T) {
	h := newHarness(t, status(http.StatusForbidden,
		`{"message":"API access is not enabled for this account.","hint":"Ask a Basa administrator to enable API access for you."}`))
	t.Setenv(config.EnvVarToken, "42|noflag")

	_, stderr, code := h.run("me", "--env", "local")

	if code != 4 {
		t.Fatalf("exit %d, want 4", code)
	}
	if !strings.Contains(stderr, "API access is not enabled") {
		t.Errorf("should surface the server's message, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "administrator") {
		t.Errorf("should surface the server's hint, got:\n%s", stderr)
	}
}

// 3 and 4 must stay distinct: one means "log in again", the other means "ask a
// human for access". Collapsing them would tell the operator to do the wrong
// thing.
func TestAuthAndForbiddenUseDifferentExitCodes(t *testing.T) {
	unauth := newHarness(t, status(http.StatusUnauthorized, `{}`))
	t.Setenv(config.EnvVarToken, "42|t")
	_, _, authCode := unauth.run("me", "--env", "local")

	forbidden := newHarness(t, status(http.StatusForbidden, `{}`))
	t.Setenv(config.EnvVarToken, "42|t")
	_, _, forbiddenCode := forbidden.run("me", "--env", "local")

	if authCode == forbiddenCode {
		t.Fatalf("401 and 403 both exit %d; they must differ", authCode)
	}
}

func TestNotFoundExitsTwo(t *testing.T) {
	h := newHarness(t, status(http.StatusNotFound, `{}`))
	t.Setenv(config.EnvVarToken, "42|t")

	_, _, code := h.run("me", "--env", "local")

	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRateLimitIsExplainedInPlainLanguage(t *testing.T) {
	h := newHarness(t, status(http.StatusTooManyRequests, `{}`))
	t.Setenv(config.EnvVarToken, "42|t")

	_, stderr, code := h.run("me", "--env", "local")

	if code == 0 {
		t.Fatal("rate limiting must not exit 0")
	}
	if !strings.Contains(strings.ToLower(stderr), "wait") {
		t.Errorf("should tell the operator to wait, got:\n%s", stderr)
	}
}

// The server says how long to wait. Saying "wait a minute" when it said 34
// seconds is not wrong so much as useless: the operator cannot tell whether
// they are 2 seconds or 2 minutes from being able to work again.
func TestRateLimitNamesTheWaitTheServerAskedFor(t *testing.T) {
	h := newHarness(t, statusWithHeaders(
		http.StatusTooManyRequests,
		map[string]string{"Retry-After": "34"},
		`{}`,
	))
	t.Setenv(config.EnvVarToken, "42|t")

	_, stderr, code := h.run("me", "--env", "local")

	if code == 0 {
		t.Fatal("rate limiting must not exit 0")
	}
	if !strings.Contains(stderr, "34 seconds") {
		t.Errorf("should name the wait the server asked for, got:\n%s", stderr)
	}
}

// Sixty seconds is a minute, and reads better as one.
func TestRateLimitRendersAWholeMinuteAsMinutes(t *testing.T) {
	h := newHarness(t, statusWithHeaders(
		http.StatusTooManyRequests,
		map[string]string{"Retry-After": "60"},
		`{}`,
	))
	t.Setenv(config.EnvVarToken, "42|t")

	_, stderr, _ := h.run("me", "--env", "local")

	if !strings.Contains(stderr, "1 minute") {
		t.Errorf("60 seconds should read as a minute, got:\n%s", stderr)
	}
}

// No header, or a header nobody can parse, must not produce an invented number.
func TestRateLimitStaysVagueWhenTheServerDoesNotSay(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"absent":      {},
		"unparseable": {"Retry-After": "soon"},
		"elapsed":     {"Retry-After": "Mon, 02 Jan 2006 15:04:05 GMT"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, statusWithHeaders(http.StatusTooManyRequests, headers, `{}`))
			t.Setenv(config.EnvVarToken, "42|t")

			_, stderr, code := h.run("me", "--env", "local")

			if code == 0 {
				t.Fatal("rate limiting must not exit 0")
			}
			if !strings.Contains(stderr, "Wait a minute") {
				t.Errorf("want the vague sentence, got:\n%s", stderr)
			}
		})
	}
}

// A script backing off needs the wait on stdout, not just in the human text.
func TestRateLimitWaitIsReadableInJSONMode(t *testing.T) {
	h := newHarness(t, statusWithHeaders(
		http.StatusTooManyRequests,
		map[string]string{"Retry-After": "5"},
		`{}`,
	))
	t.Setenv(config.EnvVarToken, "42|t")

	stdout, _, _ := h.run("me", "--env", "local", "--json")

	var payload struct {
		Error struct {
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if !strings.Contains(payload.Error.Hint, "5 seconds") {
		t.Errorf("hint should carry the wait, got %q", payload.Error.Hint)
	}
}

func TestServerErrorTellsTheOperatorItIsNotTheirFault(t *testing.T) {
	h := newHarness(t, status(http.StatusInternalServerError, `{}`))
	t.Setenv(config.EnvVarToken, "42|t")

	_, stderr, code := h.run("me", "--env", "local")

	if code == 0 {
		t.Fatal("a 500 must not exit 0")
	}
	if !strings.Contains(stderr, "not something you can fix") {
		t.Errorf("got:\n%s", stderr)
	}
}

func TestJSONModeStillEmitsAParseableErrorOnStdout(t *testing.T) {
	h := newHarness(t, status(http.StatusForbidden, `{"message":"nope"}`))
	t.Setenv(config.EnvVarToken, "42|t")

	stdout, _, code := h.run("me", "--env", "local", "--json")

	if code != 4 {
		t.Fatalf("exit %d, want 4", code)
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not valid JSON on the error path: %v\n%s", err, stdout)
	}
	if payload.Error.Message == "" {
		t.Fatalf("error object has no message: %s", stdout)
	}
}

// --- environment safety ----------------------------------------------------

func TestMissingEnvironmentIsAUsageErrorNotAGuess(t *testing.T) {
	h := newHarness(t, okHandler)
	t.Setenv(config.EnvVarToken, "42|token")

	_, stderr, code := h.run("me")

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "--env") {
		t.Errorf("should point at --env, got:\n%s", stderr)
	}
}

func TestUnknownEnvironmentListsTheKnownOnes(t *testing.T) {
	h := newHarness(t, okHandler)
	t.Setenv(config.EnvVarToken, "42|token")

	_, stderr, code := h.run("me", "--env", "prod")

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "local") {
		t.Errorf("should list the known environments, got:\n%s", stderr)
	}
}

// The active environment must be visible in the output, so an operator can
// always see which system they are looking at.
func TestActiveEnvironmentIsVisibleInOutput(t *testing.T) {
	h := newHarness(t, okHandler)
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, _, _ := h.run("me", "--env", "local")

	if !strings.Contains(stdout, "local") {
		t.Errorf("output should name the active environment, got:\n%s", stdout)
	}
}

func TestNotLoggedInIsDistinctFromARejectedToken(t *testing.T) {
	h := newHarness(t, okHandler)
	// No BASA_TOKEN and nothing stored.

	_, stderr, code := h.run("me", "--env", "local")

	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	if !strings.Contains(stderr, "not logged in") {
		t.Errorf("should say you are not logged in, got:\n%s", stderr)
	}
}

// --- basics ----------------------------------------------------------------

func TestBareInvocationShowsHelpAndSucceeds(t *testing.T) {
	h := newHarness(t, okHandler)

	stdout, _, code := h.run()

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(stdout, "basa") {
		t.Errorf("expected help text, got:\n%s", stdout)
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	h := newHarness(t, okHandler)

	_, _, code := h.run("frobnicate")

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestVersionPrints(t *testing.T) {
	h := newHarness(t, okHandler)

	stdout, _, code := h.run("version")

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(stdout, "basa") {
		t.Errorf("got %q", stdout)
	}
}

// --- token expiry ----------------------------------------------------------

// The server computes the effective expiry — the earlier of the token's own
// column and the global session window — so `auth status` has a real timestamp
// to show and must show it rather than a description.
func TestStatusShowsTheExpiryTheServerReports(t *testing.T) {
	const body = `{"data":{"id":42,"name":"Dana Reed","email":"dana@example.test",
	  "teams":[{"id":7,"name":"Acme Agency","personal":false}],
	  "token":{"name":"Basa CLI (paired)","abilities":["read"],
	  "expires_at":"2026-09-01T18:30:00+00:00"}}}`

	h := newHarness(t, status(http.StatusOK, body))
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, _, code := h.run("auth", "status", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(stdout, "2026-09-01T18:30:00+00:00") {
		t.Errorf("should show the reported expiry, got:\n%s", stdout)
	}
}

// A null expiry must not become a claim. It means "unbounded" only on a server
// that computes the effective value; one reporting the raw column returns null
// for tokens that die in hours, and the client cannot tell the two apart.
func TestStatusInventsNoExpiryWhenTheServerReportsNone(t *testing.T) {
	h := newHarness(t, okHandler) // meBody carries "expires_at":null
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, _, code := h.run("auth", "status", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	for _, invented := range []string{"8 hour", "eight hour", "never"} {
		if strings.Contains(strings.ToLower(stdout), invented) {
			t.Errorf("must not name a lifetime the server did not report (%q), got:\n%s", invented, stdout)
		}
	}
}

// --- login -----------------------------------------------------------------

func TestLoginRefusesANewEnvironmentWithoutAURL(t *testing.T) {
	h := newHarness(t, okHandler)

	_, stderr, code := h.run("auth", "login", "--env", "brand-new")

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "--url") {
		t.Errorf("should ask for --url, got:\n%s", stderr)
	}
}

func TestLoginRequiresAnEnvironment(t *testing.T) {
	h := newHarness(t, okHandler)

	_, stderr, code := h.run("auth", "login")

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "--env") {
		t.Errorf("should ask for --env, got:\n%s", stderr)
	}
}

func TestLogoutIsIdempotentAndWarnsAboutServerSideValidity(t *testing.T) {
	h := newHarness(t, okHandler)

	_, stderr, code := h.run("auth", "logout", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	// The operator must not be left thinking the token is dead on the server.
	if !strings.Contains(stderr, "revoke") {
		t.Errorf("logout should say the token stays valid server-side, got:\n%s", stderr)
	}
}

// The attribution obligation is only discharged if the text ships inside the
// artefact people download, so this asserts the embedded copy is reachable and
// carries the operative clause -- not merely that a file exists in the repo.
func TestLicensesCommandPrintsTheEmbeddedNotices(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {})

	stdout, _, code := h.run("licenses")

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	// The MIT permission notice is the clause the licence requires to travel.
	if !strings.Contains(stdout, "The above copyright notice and this permission notice") {
		t.Error("the MIT permission notice should be in the output")
	}
	// One representative of each licence family we link.
	for _, want := range []string{
		"37signals LLC",  // basecamp/cli, MIT
		"Apache License", // cobra, Apache-2.0
		"Zalando SE",     // go-keyring, MIT
		"The Go Authors", // x/term and x/sys, BSD-3
		"Daniel Joos",    // wincred, MIT (Windows-only, embedded anyway)
		"Georg Reinke",   // godbus, BSD-2 (Linux-only, embedded anyway)
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in the notices", want)
		}
	}
}
