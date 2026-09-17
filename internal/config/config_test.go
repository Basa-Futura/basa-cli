package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points config at a throwaway directory and forces file-based
// credential storage, so tests never touch the developer's real keychain or
// config. Forcing the file path is also deliberate: it is the backend with the
// stricter contract, so it is the one worth testing.
func isolate(t *testing.T) *Config {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(EnvVarNoKeyring, "1")
	t.Setenv(EnvVarToken, "")
	t.Setenv(EnvVarEnvironment, "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoadOnAFreshMachineIsEmptyNotAnError(t *testing.T) {
	cfg := isolate(t)

	if len(cfg.Environments) != 0 {
		t.Fatalf("expected no environments, got %v", cfg.Environments)
	}
}

// The token must survive a save/load cycle through credstore's FILE backend.
//
// Regression test. credstore.Save takes a []byte, but its file backend wraps
// every value in a json.RawMessage, so a bare token string fails to marshal
// while the keyring backend accepts it happily. That split meant login worked
// on a Mac with a keychain and failed on a machine without one. This test
// exists to make that failure impossible to reintroduce.
func TestTokenRoundTripsThroughFileStorage(t *testing.T) {
	cfg := isolate(t)

	const token = "42|aBcDeF0123456789wXyZ"

	if err := cfg.SaveToken("staging", token, ""); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, err := cfg.Token("staging")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != token {
		t.Fatalf("token did not survive the round trip: got %q want %q", got, token)
	}
}

func TestTokenIsNotWrittenToTheConfigFile(t *testing.T) {
	cfg := isolate(t)

	const token = "42|aBcDeF0123456789wXyZ"
	cfg.SetEnvironment("staging", "https://staging.example")

	if err := cfg.SaveToken("staging", token, ""); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(cfg.Path())
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("the token leaked into config.json")
	}
}

func TestCredentialFileIsNotWorldReadable(t *testing.T) {
	cfg := isolate(t)

	if err := cfg.SaveToken("staging", "42|secret", ""); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	info, err := os.Stat(filepath.Join(Dir(), "credentials.json"))
	if err != nil {
		t.Fatalf("stat credentials: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("credentials.json is %04o, want 0600", perm)
	}
}

func TestEnvVarTokenWinsOverStoredCredentials(t *testing.T) {
	cfg := isolate(t)

	if err := cfg.SaveToken("staging", "stored-token", ""); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	t.Setenv(EnvVarToken, "env-token")

	got, err := cfg.Token("staging")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "env-token" {
		t.Fatalf("got %q, want the env var to win", got)
	}
}

func TestDeleteTokenIsIdempotent(t *testing.T) {
	cfg := isolate(t)

	// Logout must reach a known state even when there was nothing stored,
	// otherwise an operator can get stuck unable to reset.
	if err := cfg.DeleteToken("never-logged-in"); err != nil {
		t.Fatalf("DeleteToken on absent credential: %v", err)
	}

	if err := cfg.SaveToken("staging", "42|secret", ""); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	if err := cfg.DeleteToken("staging"); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}
	if _, err := cfg.Token("staging"); err == nil {
		t.Fatal("token still readable after delete")
	}
}

// --- environment resolution ------------------------------------------------

// A machine that has never been configured resolves to production, because
// production is compiled in and is therefore the only thing it knows. The
// command then fails on "not logged in", which is the accurate complaint —
// where this once answered "pass --env with one of: production", a question
// with exactly one possible answer.
func TestResolveOnAFreshMachineChoosesProduction(t *testing.T) {
	cfg := isolate(t)

	name, env, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != EnvProduction {
		t.Fatalf("got %q, want %q", name, EnvProduction)
	}
	if env.URL != ProductionURL {
		t.Fatalf("got URL %q, want the compiled-in %q", env.URL, ProductionURL)
	}
}

// Production is known without any configuration at all, which is what spares a
// non-staff operator from ever typing a hostname.
func TestProductionIsKnownWithoutConfiguration(t *testing.T) {
	cfg := isolate(t)

	env, ok := cfg.Lookup(EnvProduction)
	if !ok || env.URL != ProductionURL {
		t.Fatalf("Lookup(production) = %q, %v", env.URL, ok)
	}
	if _, ok := cfg.Lookup("staging"); ok {
		t.Fatal("only production is built in")
	}
}

// Configuring production explicitly overrides the compiled-in URL, which is how
// it gets corrected without shipping a new binary.
func TestConfiguredProductionURLBeatsTheBuiltIn(t *testing.T) {
	cfg := isolate(t)
	cfg.SetEnvironment(EnvProduction, "https://prod-clone.example")

	_, env, err := cfg.Resolve(EnvProduction)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if env.URL != "https://prod-clone.example" {
		t.Fatalf("built-in URL won over the configured one: %q", env.URL)
	}
}

// The single most important behaviour in this package: with exactly one
// environment configured, Resolve still refuses to assume it. An operator who
// habitually omits --env is one who will omit it on the day a second
// environment — production — exists.
func TestResolveRefusesToGuessEvenWithOnlyOneEnvironment(t *testing.T) {
	cfg := isolate(t)
	cfg.SetEnvironment("staging", "https://staging.example")

	_, _, err := cfg.Resolve("")
	if err == nil {
		t.Fatal("expected an error; a lone environment must not become an implicit default")
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Fatalf("error should name the available environments, got: %v", err)
	}
}

func TestResolveUsesTheFlag(t *testing.T) {
	cfg := isolate(t)
	cfg.SetEnvironment("staging", "https://staging.example")

	name, env, err := cfg.Resolve("staging")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != "staging" || env.URL != "https://staging.example" {
		t.Fatalf("got %q / %q", name, env.URL)
	}
}

func TestResolveFallsBackToTheEnvVar(t *testing.T) {
	cfg := isolate(t)
	cfg.SetEnvironment("staging", "https://staging.example")
	t.Setenv(EnvVarEnvironment, "staging")

	name, _, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != "staging" {
		t.Fatalf("got %q, want staging", name)
	}
}

func TestResolveNamesTheKnownEnvironmentsWhenAskedForAnUnknownOne(t *testing.T) {
	cfg := isolate(t)
	cfg.SetEnvironment("staging", "https://staging.example")
	cfg.SetEnvironment("local", "http://localhost:8000")

	_, _, err := cfg.Resolve("typo")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"local", "staging"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should list %q, got: %v", want, err)
		}
	}
}

func TestSetEnvironmentStripsTrailingSlash(t *testing.T) {
	cfg := isolate(t)
	cfg.SetEnvironment("staging", "https://staging.example/")

	if got := cfg.Environments["staging"].URL; got != "https://staging.example" {
		t.Fatalf("got %q", got)
	}
}

func TestConfigSurvivesSaveAndReload(t *testing.T) {
	cfg := isolate(t)
	cfg.SetEnvironment("staging", "https://staging.example")

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Environments["staging"].URL != "https://staging.example" {
		t.Fatalf("environment did not persist: %v", reloaded.Environments)
	}
}

// --- the non-staff production fallback -------------------------------------
//
// These pin the one case where Resolve chooses instead of asking. Read them
// alongside TestResolveRefusesToGuessEvenWithOnlyOneEnvironment above: that
// test is still the rule, and this is the exception, and the difference between
// them is entirely who is logged in.

// loggedInTo stores a credential so Resolve can read an identity back out.
func loggedInTo(t *testing.T, cfg *Config, env, url, email string) {
	t.Helper()

	cfg.SetEnvironment(env, url)
	if err := cfg.SaveToken(env, "42|token", email); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
}

func TestResolveChoosesProductionForANonStaffOperator(t *testing.T) {
	cfg := isolate(t)
	loggedInTo(t, cfg, EnvProduction, "https://app.example", "client@agency.example")

	name, env, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != EnvProduction {
		t.Fatalf("got %q, want %q", name, EnvProduction)
	}
	if env.URL != "https://app.example" {
		t.Fatalf("got URL %q", env.URL)
	}
}

// Staff hold several environments, so for them the question is the whole point.
func TestResolveStillRefusesToGuessForStaff(t *testing.T) {
	cfg := isolate(t)
	loggedInTo(t, cfg, EnvProduction, "https://app.example", "ryan@basafutura.com")

	if _, _, err := cfg.Resolve(""); err == nil {
		t.Fatal("expected an error; staff must name their environment")
	}
}

// A credential predating the email field reads as unknown, and unknown no
// longer refuses: with production the only environment on this machine, it is
// the only thing the operator could have meant. The environment count below is
// what carries the safety now.
func TestResolveStillChoosesProductionWhenTheIdentityIsUnknown(t *testing.T) {
	cfg := isolate(t)
	loggedInTo(t, cfg, EnvProduction, ProductionURL, "")

	name, _, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != EnvProduction {
		t.Fatalf("got %q, want %q", name, EnvProduction)
	}
}

// THE safety property, now that production is compiled in and always known:
// the moment a second environment is configured, the choice becomes real and is
// asked about again — whoever is logged in. Only staff can configure a second
// one, because staging and local refuse everyone else.
func TestASecondEnvironmentTurnsTheFallbackOff(t *testing.T) {
	cfg := isolate(t)
	loggedInTo(t, cfg, EnvProduction, ProductionURL, "client@agency.example")

	if _, _, err := cfg.Resolve(""); err != nil {
		t.Fatalf("precondition: production alone should resolve: %v", err)
	}

	cfg.SetEnvironment("staging", "https://staging.example")

	if _, _, err := cfg.Resolve(""); err == nil {
		t.Fatal("expected an error once a second environment exists")
	}
}

// Logged in to staging only: production is still *known* (it is built in), but
// it is not the only configured environment, so nothing is chosen.
func TestResolveRefusesWhenLoggedInElsewhere(t *testing.T) {
	cfg := isolate(t)
	loggedInTo(t, cfg, "staging", "https://staging.example", "client@agency.example")

	if _, _, err := cfg.Resolve(""); err == nil {
		t.Fatal("expected an error; a configured staging means the question is real")
	}
}

// Automation must stay explicit, and this is also what keeps a BASA_TOKEN run
// from probing the keyring — the credential read is the only thing that would.
func TestResolveRefusesForAutomationEvenWhenNonStaff(t *testing.T) {
	cfg := isolate(t)
	loggedInTo(t, cfg, EnvProduction, "https://app.example", "client@agency.example")
	t.Setenv(EnvVarToken, "42|from-env")

	if _, _, err := cfg.Resolve(""); err == nil {
		t.Fatal("expected an error; BASA_TOKEN callers must name their environment")
	}
}

// The fallback is a floor, never a ceiling: anything the operator says wins.
func TestExplicitSelectionOutranksTheFallback(t *testing.T) {
	cfg := isolate(t)
	loggedInTo(t, cfg, EnvProduction, "https://app.example", "client@agency.example")
	cfg.SetEnvironment("staging", "https://staging.example")

	name, _, err := cfg.Resolve("staging")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != "staging" {
		t.Fatalf("flag lost to the fallback: got %q", name)
	}

	t.Setenv(EnvVarEnvironment, "staging")
	if name, _, err = cfg.Resolve(""); err != nil || name != "staging" {
		t.Fatalf("BASA_ENV lost to the fallback: got %q, %v", name, err)
	}
}

// The email must survive credstore's FILE backend for the same reason the token
// must — see TestTokenRoundTripsThroughFileStorage.
func TestEmailRoundTripsThroughFileStorage(t *testing.T) {
	cfg := isolate(t)

	if err := cfg.SaveToken("staging", "42|secret", "  Client@Agency.Example  "); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	if got := cfg.Email("staging"); got != "Client@Agency.Example" {
		t.Fatalf("got %q", got)
	}
	if got := cfg.Email("never-logged-in"); got != "" {
		t.Fatalf("absent credential should give no email, got %q", got)
	}
}

func TestIsStaffEmail(t *testing.T) {
	for _, tc := range []struct {
		email string
		staff bool
	}{
		{"ryan@basafutura.com", true},
		{"  Staff@BasaFutura.COM  ", true},
		{"client@agency.example", false},
		{"", false},
		// The "@" in the suffix is what stops a lookalike domain from passing.
		{"attacker@notbasafutura.com", false},
		// A subdomain is not the staff domain either, matching User::isStaff().
		{"someone@mail.basafutura.com", false},
		{"basafutura.com", false},
		{"someone@basafutura.com.evil.example", false},
	} {
		if got := IsStaffEmail(tc.email); got != tc.staff {
			t.Errorf("IsStaffEmail(%q) = %v, want %v", tc.email, got, tc.staff)
		}
	}
}
