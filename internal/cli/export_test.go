package cli

import (
	"io"
	"testing"
)

// Test-only access to the update seams. Each restores what it replaced, so a
// test cannot leak a fake release server or binary path into the next one.

func SetReleasesURL(t testing.TB, url string) {
	old := releasesURL
	releasesURL = url
	t.Cleanup(func() { releasesURL = old })
}

func SetExecutable(t testing.TB, path string) {
	old := executable
	executable = func() (string, error) { return path, nil }
	t.Cleanup(func() { executable = old })
}

func SetVersion(t testing.TB, v string) {
	old := Version
	Version = v
	t.Cleanup(func() { Version = old })
}

// AssumeTerminal makes stderr count as a person's terminal.
func AssumeTerminal(t testing.TB) { SetTerminal(t, true) }

func SetTerminal(t testing.TB, is bool) {
	old := isTerminal
	isTerminal = func(io.Writer) bool { return is }
	t.Cleanup(func() { isTerminal = old })
}

// CountSpawns replaces starting the background check with counting the
// starts, and returns the counter.
func CountSpawns(t testing.TB) *int {
	n := new(int)
	old := spawnCheck
	spawnCheck = func() error { *n++; return nil }
	t.Cleanup(func() { spawnCheck = old })
	return n
}

var UpdateCheckWanted = updateCheckWanted
