// Package config stores which Basa environments this machine knows about and
// holds the token for each.
//
// Environment targeting is a safety feature here, not a convenience. There is
// deliberately NO fallback environment: if the operator does not say which one
// they mean, the command fails and tells them to. A CLI that silently defaults
// to production is one typo away from an incident, and these operators are not
// engineers who would spot the difference in the output.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/basecamp/cli/credstore"
	"github.com/zalando/go-keyring"
)

const (
	// EnvVarEnvironment names the active environment without a flag.
	EnvVarEnvironment = "BASA_ENV"

	// EnvVarToken bypasses stored credentials entirely. This is what CI and
	// scripts use, and it is why no automated caller ever needs a keyring.
	EnvVarToken = "BASA_TOKEN"

	// EnvVarNoKeyring forces file storage.
	EnvVarNoKeyring = "BASA_NO_KEYRING"

	serviceName = "basa"
)

// Environment is one Basa deployment this machine can talk to.
type Environment struct {
	URL string `json:"url"`
}

// Config is the on-disk file. It holds no secrets — tokens live in the
// keyring, or in a 0600 file managed by credstore.
type Config struct {
	Environments map[string]Environment `json:"environments"`

	path string

	// The credential store is built on first use, never in Load. Its
	// constructor probes the system keyring with a write and a delete, and Load
	// runs for every command that reads configuration — so an eager store meant
	// a BASA_TOKEN automation run, which never touches a stored credential,
	// still wrote to the keychain on every invocation.
	storeOpts credstore.StoreOptions
	storeOnce sync.Once
	store     *credstore.Store
}

// st returns the credential store, building it on first use.
func (c *Config) st() *credstore.Store {
	c.storeOnce.Do(func() { c.store = credstore.NewStore(c.storeOpts) })
	return c.store
}

// Dir is the config directory, honouring XDG when it is set.
func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "basa")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".basa"
	}
	return filepath.Join(home, ".config", "basa")
}

// Load reads the config, returning an empty one if the file does not exist yet.
func Load() (*Config, error) {
	dir := Dir()
	path := filepath.Join(dir, "config.json")

	cfg := &Config{
		Environments: map[string]Environment{},
		path:         path,
		storeOpts: credstore.StoreOptions{
			ServiceName:   serviceName,
			DisableEnvVar: EnvVarNoKeyring,
			FallbackDir:   dir,
		},
	}

	// #nosec G304 -- not caller-supplied. path is filepath.Join(Dir(), "config.json"):
	// a constant filename under a directory derived from XDG_CONFIG_HOME, else the
	// user's home. Nothing outside this process contributes to it, the read happens
	// with the invoking user's own permissions, and anyone able to set this
	// process's environment can already run any binary as that user -- or set
	// BASA_TOKEN, which bypasses this file entirely.
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if cfg.Environments == nil {
		cfg.Environments = map[string]Environment{}
	}

	return cfg, nil
}

// ensureDir creates the config directory at 0700.
//
// Both Save and SaveToken need it, and SaveToken is called first during login —
// credstore's file fallback writes into this directory and does not create it,
// so without this a first-ever login fails with "could not save the token" on a
// machine with no keyring.
func (c *Config) ensureDir() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	return nil
}

// Save writes the config file.
func (c *Config) Save() error {
	if err := c.ensureDir(); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.path, append(raw, '\n'), 0o600)
}

// Path is the config file location, for `auth status` to report.
func (c *Config) Path() string { return c.path }

// Names lists known environments, sorted.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Environments))
	for name := range c.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Resolve determines the active environment from an explicit flag or BASA_ENV.
//
// It never guesses. With nothing set it returns an error naming the known
// environments, even when only one is configured — an operator who habitually
// omits --env is an operator who will omit it on the day a second environment
// exists.
func (c *Config) Resolve(flag string) (string, Environment, error) {
	name := flag
	if name == "" {
		name = os.Getenv(EnvVarEnvironment)
	}

	if name == "" {
		if len(c.Environments) == 0 {
			return "", Environment{}, fmt.Errorf("no environments configured; run: basa auth login --env <name> --url <url>")
		}
		return "", Environment{}, fmt.Errorf("no environment selected; pass --env with one of: %s", strings.Join(c.Names(), ", "))
	}

	env, ok := c.Environments[name]
	if !ok {
		if len(c.Environments) == 0 {
			return "", Environment{}, fmt.Errorf("unknown environment %q; none are configured yet", name)
		}
		return "", Environment{}, fmt.Errorf("unknown environment %q; known: %s", name, strings.Join(c.Names(), ", "))
	}

	return name, env, nil
}

// SetEnvironment records an environment's URL.
func (c *Config) SetEnvironment(name, url string) {
	c.Environments[name] = Environment{URL: strings.TrimRight(url, "/")}
}

// --- credentials -----------------------------------------------------------

func credKey(env string) string { return "token:" + env }

// credential is what gets stored per environment.
//
// It is a JSON object rather than the bare token string for a non-obvious
// reason worth recording. credstore.Save takes a []byte and reads like it
// accepts opaque bytes — but its FILE backend wraps each value in a
// json.RawMessage, so a non-JSON payload fails to marshal. Its KEYRING backend
// accepts any string. Storing a raw token therefore works on a Mac with a
// keychain and fails on any machine without one, which is the worst possible
// split: it passes locally and breaks for the operator who most needs it to
// work. Always hand credstore valid JSON.
type credential struct {
	Token string `json:"token"`
}

// Token returns the token for an environment. BASA_TOKEN wins over anything
// stored, so an automated caller never touches the keyring.
func (c *Config) Token(env string) (string, error) {
	if t := os.Getenv(EnvVarToken); t != "" {
		return strings.TrimSpace(t), nil
	}

	raw, err := c.st().Load(credKey(env))
	if err != nil || len(raw) == 0 {
		return "", errors.New("no stored token")
	}

	var cred credential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return "", errors.New("stored credential is unreadable")
	}
	if cred.Token == "" {
		return "", errors.New("no stored token")
	}

	return strings.TrimSpace(cred.Token), nil
}

// SaveToken stores a token for an environment.
func (c *Config) SaveToken(env, token string) error {
	if err := c.ensureDir(); err != nil {
		return err
	}

	raw, err := json.Marshal(credential{Token: token})
	if err != nil {
		return err
	}

	return c.st().Save(credKey(env), raw)
}

// DeleteToken removes the stored token for an environment. Absent is success —
// logout must be idempotent so an operator can always reach a known state —
// but only absent. A keyring that refuses, or a credentials file that cannot be
// rewritten, is a failure to report: "Removed" over a token still on disk is
// the one thing logout must never say.
func (c *Config) DeleteToken(env string) error {
	err := c.st().Delete(credKey(env))
	if err == nil || errors.Is(err, keyring.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// UsingKeyring reports whether the system keyring is in use.
func (c *Config) UsingKeyring() bool { return c.st().UsingKeyring() }

// FallbackWarning is non-empty when credentials landed in a file instead of the
// keyring. The operator is told, because it changes where their token lives.
func (c *Config) FallbackWarning() string { return c.st().FallbackWarning() }
