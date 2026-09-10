package config

import "testing"

// Load must not build the credential store. credstore.NewStore probes the
// system keyring with a write and a delete, and Load runs for every command
// that reads configuration — so an eager store meant `basa --help` touched the
// keychain, and an automation run carrying BASA_TOKEN, which never reads a
// stored credential, probed it on every invocation. White-box on purpose: the
// probe itself is unobservable from outside without a real keychain.
func TestLoadDoesNotTouchTheCredentialStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvVarNoKeyring, "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.store != nil {
		t.Fatal("Load built the credential store; it must wait for first use")
	}

	// A token from the environment never needs the store either.
	t.Setenv(EnvVarToken, "42|from-env")
	if tok, err := cfg.Token("any"); err != nil || tok != "42|from-env" {
		t.Fatalf("Token from env = %q, %v", tok, err)
	}
	if cfg.store != nil {
		t.Fatal("reading BASA_TOKEN built the credential store")
	}

	// Saving is the first operation that genuinely needs it.
	if err := cfg.SaveToken("any", "42|stored"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	if cfg.store == nil {
		t.Fatal("SaveToken should have built the store")
	}
}
