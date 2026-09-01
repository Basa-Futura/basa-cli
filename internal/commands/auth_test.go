package commands

import (
	"strings"
	"testing"
)

// The end-to-end login behaviour is tested in internal/cli against a fake
// server. These two are here because they are decisions rather than behaviour:
// a URL that must match a route the server owns, and a gate whose four
// combinations cannot all be reached through a test binary — go test's stdin is
// never a terminal, so the interactive half is unreachable from the harness.

func TestPairURL(t *testing.T) {
	cases := map[string]string{
		"https://staging.basa.example":  "https://staging.basa.example/cli/pair",
		"https://staging.basa.example/": "https://staging.basa.example/cli/pair",
		// Trailing slashes are already stripped by config.SetEnvironment; this
		// only proves the join does not add a second one if that ever changes.
		"https://staging.basa.example///": "https://staging.basa.example/cli/pair",
		// An environment served under a path prefix keeps it.
		"http://127.0.0.1:8002":     "http://127.0.0.1:8002/cli/pair",
		"https://example.test/basa": "https://example.test/basa/cli/pair",
	}

	for base, want := range cases {
		if got := pairURL(base); got != want {
			t.Errorf("pairURL(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestShouldOpenBrowser(t *testing.T) {
	cases := []struct {
		interactive bool
		noBrowser   bool
		want        bool
		why         string
	}{
		{true, false, true, "at a terminal and not asked to stay put"},
		{true, true, false, "--no-browser is the operator saying don't"},
		{false, false, false, "piped: automation, and CI must not open a browser"},
		{false, true, false, "piped and told not to"},
	}

	for _, tc := range cases {
		if got := shouldOpenBrowser(tc.interactive, tc.noBrowser); got != tc.want {
			t.Errorf("shouldOpenBrowser(interactive=%v, noBrowser=%v) = %v, want %v — %s",
				tc.interactive, tc.noBrowser, got, tc.want, tc.why)
		}
	}
}

// Both ways a token can arrive — the pipe and the no-echo prompt — go through
// normalizeToken, so this pins the rule for both. The interactive path cannot be
// reached from a test binary at all, which is the reason the rule lives in one
// function rather than in two branches that happen to agree today.
func TestNormalizeToken(t *testing.T) {
	const token = "14|vgxvg0gp2KVP8bEVkEYzSjO3QrStUvWxYz012345"

	cases := map[string]string{
		token:                 token,
		token + "\n":          token,
		token + "\r\n":        token, // a Windows clipboard
		"  " + token + "  ":   token,
		"\t" + token + "\n\n": token,
	}

	for raw, want := range cases {
		if got := normalizeToken(raw); got != want {
			t.Errorf("normalizeToken(%q) = %q, want %q", raw, got, want)
		}
	}

	// The pipe and everything around it must survive untouched.
	if got := normalizeToken(token); !strings.Contains(got, "|") {
		t.Errorf("the pipe is part of the credential, got %q", got)
	}
	if got := normalizeToken(" " + token + "\n"); strings.Count(got, "|") != 1 {
		t.Errorf("expected exactly one pipe, got %q", got)
	}
}

// The openers act on filesystem paths and registered URI schemes, not just web
// pages, so anything but http/https is refused and the printed URL carries it.
func TestIsBrowsable(t *testing.T) {
	cases := map[string]bool{
		"https://staging.basa.example/cli/pair": true,
		"http://127.0.0.1:8002/cli/pair":        true,
		"HTTPS://staging.basa.example/cli/pair": true, // url.Parse lowercases the scheme

		// Would make `open` act on the filesystem.
		"/etc/passwd/cli/pair":          false,
		"file:///etc/passwd/cli/pair":   false,
		"staging.basa.example/cli/pair": false, // no scheme: a relative path
		"/Applications/Calculator.app":  false,

		// Would invoke whatever registered the scheme.
		"myapp://open/cli/pair": false,
		"javascript:alert(1)":   false,
		"ssh://host/cli/pair":   false,

		// Nothing to open.
		"":             false,
		"https://":     false,
		"http:///path": false,
	}

	for target, want := range cases {
		if got := isBrowsable(target); got != want {
			t.Errorf("isBrowsable(%q) = %v, want %v", target, got, want)
		}
	}
}
