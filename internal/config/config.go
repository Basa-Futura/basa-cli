// Package config stores which Basa environments this machine knows about and
// holds the token for each.
//
// Environment targeting is a safety feature here, not a convenience. The rule
// is that the operator names the environment: a CLI that silently defaults to
// production is one typo away from an incident, and these operators are not
// engineers who would spot the difference in the output.
//
// There is exactly one exception, and it is narrower than it looks. An operator
// whose stored production identity is NOT a Basa staff address has no other
// environment to confuse production with — staging and local are staff-only, so
// a non-staff operator holds one deployment and one token. For them "which
// environment did you mean" is a question with a single possible answer, and
// asking it every time is friction that teaches nothing. For staff, who hold
// several, the question is the whole point and is still asked.
//
// The asymmetry is deliberate: the guard rail exists for people who can steer
// into the wrong lane, and it stays up for exactly those people.
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

	// EnvProduction is the one environment name that can be chosen for an
	// operator rather than by them. It is a literal name, not a guess about
	// which URL looks like production: `basa auth login --env production`
	// creates it, anything else does not, and an operator who paired production
	// under another name simply keeps naming it.
	EnvProduction = "production"

	// ProductionURL is where production is, and it is compiled in rather than
	// configured. An operator outside Basa has exactly one valid answer to
	// "which host", so asking them for it is not a safety check — it is a
	// chance to typo a hostname into a login flow, which is the one place a
	// wrong host is genuinely dangerous.
	//
	// It is a default, not a constant: an explicit --url still wins and is
	// written to config.json, which is how staff point `production` somewhere
	// else and how this is corrected without a new build.
	ProductionURL = "https://app.basafutura.com"

	// staffDomain marks a Basa operator, who is assumed to reach more than one
	// environment. The canonical rule is the web application's User::isStaff()
	// (app/Models/User.php); this mirrors it rather than sharing it, because the
	// CLI must decide before it has talked to any server. Keep the two in step.
	staffDomain = "@basafutura.com"
)

// IsStaffEmail reports whether an address belongs to a Basa operator.
func IsStaffEmail(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), staffDomain)
}

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

	// #nosec G304 -- the path crosses no privilege boundary. It is
	// filepath.Join(Dir(), "config.json"): a constant filename under a directory
	// taken from XDG_CONFIG_HOME, else the user's home. That environment is
	// inherited from whoever invoked the CLI, which is the same principal the
	// read then runs as -- this binary is not setuid, and no component of the
	// path ever arrives from a flag, an argument, or a server response. An
	// attacker positioned to redirect it is already able to run any binary as
	// that user, so the redirection gains them nothing.
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

// Lookup returns an environment by name. Configured environments win, so an
// explicit --url always beats the compiled-in default; production is known even
// on a machine that has never been configured at all.
func (c *Config) Lookup(name string) (Environment, bool) {
	if env, ok := c.Environments[name]; ok {
		return env, true
	}
	if name == EnvProduction {
		return Environment{URL: ProductionURL}, true
	}
	return Environment{}, false
}

// Names lists known environments, sorted. Production is always among them.
func (c *Config) Names() []string {
	seen := make(map[string]struct{}, len(c.Environments)+1)
	for name := range c.Environments {
		seen[name] = struct{}{}
	}
	seen[EnvProduction] = struct{}{}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Resolve determines the active environment from an explicit flag, BASA_ENV, or
// — for a non-staff operator only — the stored production environment.
//
// With nothing set and no such fallback it returns an error naming the known
// environments, even when only one is configured. That refusal is the package's
// central behaviour for staff: an operator who habitually omits --env is an
// operator who will omit it on the day a second environment exists. A non-staff
// operator cannot reach a second environment in the first place, so for them
// there is no day to guard against; see the package comment.
func (c *Config) Resolve(flag string) (string, Environment, error) {
	name := flag
	if name == "" {
		name = os.Getenv(EnvVarEnvironment)
	}
	if name == "" {
		name = c.productionFallback()
	}

	if name == "" {
		return "", Environment{}, fmt.Errorf("no environment selected; pass --env with one of: %s", strings.Join(c.Names(), ", "))
	}

	env, ok := c.Lookup(name)
	if !ok {
		return "", Environment{}, fmt.Errorf("unknown environment %q; known: %s", name, strings.Join(c.Names(), ", "))
	}

	return name, env, nil
}

// productionFallback returns EnvProduction when this operator may have it
// chosen for them, and "" when they must say which environment they mean.
//
// Every condition is a reason to stay silent rather than a reason to choose,
// which is the right default for a function that can point a command at
// production:
//
//   - BASA_TOKEN set. The caller is automation, which must be explicit for the
//     same reason a cron job should never inherit a human's habits. It also
//     keeps Load's promise that a BASA_TOKEN run never probes the keyring — the
//     credential read below is the only thing that would break it.
//   - Anything is configured besides production. This is the load-bearing one
//     now that production is compiled in: the moment a second environment
//     exists, choosing between them is a real question and gets asked. Only
//     staff can meaningfully configure a second one, since staging and local
//     refuse everyone else.
//   - A staff address stored for production. Staff reach more than one
//     deployment even when this machine has only seen one, so they keep naming
//     it.
//
// An absent email is deliberately NOT a refusal here, unlike the earlier draft
// of this function. It means a fresh machine or a credential predating the
// field, and in both cases production is still the only thing this operator
// could possibly have meant — the environment count above already carries the
// safety. Refusing would have made `basa me` on a new machine answer "pass
// --env with one of: production", which is an absurd question with one answer.
func (c *Config) productionFallback() string {
	if os.Getenv(EnvVarToken) != "" {
		return ""
	}

	for name := range c.Environments {
		if name != EnvProduction {
			return ""
		}
	}

	if IsStaffEmail(c.Email(EnvProduction)) {
		return ""
	}

	return EnvProduction
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

	// Email is the address the server reported for this token at login. It is
	// stored so Resolve can tell a staff operator from everyone else BEFORE any
	// environment has been chosen — the identity question and the environment
	// question are otherwise circular, since the only source of an email is a
	// call to a server that has already been picked.
	//
	// A credential written before this field existed unmarshals with an empty
	// Email. That is not treated as a refusal: productionFallback leans on the
	// environment count instead, so an operator whose machine knows only
	// production still has it chosen for them, logged in before this field
	// existed or not. An unknown identity only decides anything once a second
	// environment is configured, and by then the fallback is off regardless.
	Email string `json:"email,omitempty"`
}

// loadCredential reads and decodes the stored credential for an environment.
func (c *Config) loadCredential(env string) (credential, error) {
	raw, err := c.st().Load(credKey(env))
	if err != nil || len(raw) == 0 {
		return credential{}, errors.New("no stored token")
	}

	var cred credential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return credential{}, errors.New("stored credential is unreadable")
	}

	return cred, nil
}

// Token returns the token for an environment. BASA_TOKEN wins over anything
// stored, so an automated caller never touches the keyring.
func (c *Config) Token(env string) (string, error) {
	if t := os.Getenv(EnvVarToken); t != "" {
		return strings.TrimSpace(t), nil
	}

	cred, err := c.loadCredential(env)
	if err != nil {
		return "", err
	}
	if cred.Token == "" {
		return "", errors.New("no stored token")
	}

	return strings.TrimSpace(cred.Token), nil
}

// Email returns the address recorded for an environment at login, or "" when
// nothing is stored or the credential predates the field.
func (c *Config) Email(env string) string {
	cred, err := c.loadCredential(env)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cred.Email)
}

// SaveToken stores a token for an environment, alongside the address the server
// reported for it. The email is not decoration: Resolve reads it back to decide
// whether this operator must name their environment.
func (c *Config) SaveToken(env, token, email string) error {
	if err := c.ensureDir(); err != nil {
		return err
	}

	raw, err := json.Marshal(credential{Token: token, Email: strings.TrimSpace(email)})
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
