package update

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Interval is how long a successful check's answer is trusted. A day means an
// operator hears about a release within about a day of it, and GitHub hears
// from each laptop about once a day, not once per command.
const Interval = 24 * time.Hour

// RetryAfter is how long a started check holds off the next one. A check
// that never finished — offline, or GitHub slow — leaves only this mark, so it
// is retried within the hour rather than on every command, and rather than
// being silenced for a day.
const RetryAfter = time.Hour

// state is what earlier runs learned, kept between them.
type state struct {
	CheckedAt   time.Time `json:"checked_at"`
	AttemptedAt time.Time `json:"attempted_at"`
	Latest      string    `json:"latest,omitempty"`
}

// Checker decides whether an operator should be told about a newer release.
//
// The work is split in two because a command must never wait on GitHub. The
// command itself only reads the saved answer (Newer) and, when that answer is
// stale (Due), starts a separate process that asks GitHub and saves a new one
// (Refresh). A goroutine would not do: `basa version` exits in milliseconds,
// long before GitHub answers, and takes its goroutines with it — so fast
// commands would start checks that never finish. A separate process outlives
// the command that started it.
//
// The cost is that a release is announced on the first command after it is
// discovered, not the one that discovers it.
type Checker struct {
	Current   Version
	Releases  *Releases
	StatePath string
	Now       func() time.Time
}

// Newer returns the latest known release if it is newer than the running
// build. It reads only the saved answer, so it never touches the network.
func (c *Checker) Newer() (Version, bool) {
	latest, ok := Parse(c.load().Latest)
	if ok && c.Current.Less(latest) {
		return latest, true
	}
	return Version{}, false
}

// Due reports whether a check should be started: the last answer is more
// than a day old, and no check has been started in the last hour.
func (c *Checker) Due() bool {
	st := c.load()
	now := c.now()
	return now.Sub(st.CheckedAt) >= Interval && now.Sub(st.AttemptedAt) >= RetryAfter
}

// MarkAttempt records that a check has been started, so the commands that run
// while it is in flight — or after it has failed — do not start another.
func (c *Checker) MarkAttempt() {
	st := c.load()
	st.AttemptedAt = c.now()
	c.save(st)
}

// Refresh asks GitHub for the latest release and saves the answer. On failure
// nothing new is saved, and the attempt mark decides when to try again.
func (c *Checker) Refresh(ctx context.Context) error {
	v, err := c.Releases.Latest(ctx)
	if err != nil {
		return err
	}
	st := c.load()
	st.CheckedAt = c.now()
	st.Latest = v.String()
	c.save(st)
	return nil
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// load reads the saved state. A missing or unreadable file is a check that
// has never run — it is a cache, and a broken cache must never fail a command.
func (c *Checker) load() state {
	var st state
	// #nosec G304 -- StatePath is a constant filename under config.Dir(), the
	// same directory config.json is read from; no part of it comes from a flag,
	// an argument, or a server response.
	raw, err := os.ReadFile(c.StatePath)
	if err != nil {
		return state{}
	}
	if json.Unmarshal(raw, &st) != nil {
		return state{}
	}
	return st
}

// save writes the state via a rename, because a command reading it can run
// while a check is writing it, and a torn file would read back as garbage.
// Errors are dropped for the same reason load ignores them.
func (c *Checker) save(st state) {
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}

	dir := filepath.Dir(c.StatePath)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())

	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		return
	}
	_ = os.Rename(tmp.Name(), c.StatePath)
}
