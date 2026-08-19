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

	if err := cfg.SaveToken("staging", token); err != nil {
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

	if err := cfg.SaveToken("staging", token); err != nil {
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

	if err := cfg.SaveToken("staging", "42|secret"); err != nil {
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

	if err := cfg.SaveToken("staging", "stored-token"); err != nil {
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

	if err := cfg.SaveToken("staging", "42|secret"); err != nil {
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

func TestResolveRefusesToGuessWhenNothingIsConfigured(t *testing.T) {
	cfg := isolate(t)

	if _, _, err := cfg.Resolve(""); err == nil {
		t.Fatal("expected an error rather than a guess")
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
