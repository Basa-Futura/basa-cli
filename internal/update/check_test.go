package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func checker(t *testing.T, baseURL, current string) *Checker {
	t.Helper()
	v, _ := Parse(current)
	return &Checker{
		Current:   v,
		Releases:  &Releases{BaseURL: baseURL},
		StatePath: filepath.Join(t.TempDir(), "update-check.json"),
		Now:       func() time.Time { return now },
	}
}

func writeState(t *testing.T, path string, st state) {
	t.Helper()
	raw, _ := json.Marshal(st)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readState(t *testing.T, path string) state {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}
	var st state
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("parsing state: %v", err)
	}
	return st
}

func TestNewerReadsTheSavedAnswer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current string
		saved   string
		want    Version
		ok      bool
	}{
		{"a newer release", "0.1.6", "v0.1.7", Version{0, 1, 7}, true},
		{"already current", "0.1.7", "v0.1.7", Version{}, false},
		{"ahead of the release", "0.1.8", "v0.1.7", Version{}, false},
		{"never checked", "0.1.6", "", Version{}, false},
	} {
		c := checker(t, "http://unused.invalid", tc.current)
		if tc.saved != "" {
			writeState(t, c.StatePath, state{CheckedAt: now, Latest: tc.saved})
		}
		got, ok := c.Newer()
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: Newer = %v, %v; want %v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNewerIgnoresACorruptStateFile(t *testing.T) {
	c := checker(t, "http://unused.invalid", "0.1.6")
	if err := os.WriteFile(c.StatePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Newer(); ok {
		t.Errorf("Newer = %v, true; want nothing from a corrupt file", got)
	}
	if !c.Due() {
		t.Error("Due = false; a corrupt file should count as never checked")
	}
}

func TestDue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state *state
		want  bool
	}{
		{"never checked", nil, true},
		{"checked 23 hours ago", &state{CheckedAt: now.Add(-23 * time.Hour)}, false},
		{"checked 25 hours ago", &state{CheckedAt: now.Add(-25 * time.Hour)}, true},
		// A check started recently holds off the next one whether it finished
		// or not, so a laptop that is offline does not start one per command.
		{"stale, but a check started 10 minutes ago", &state{CheckedAt: now.Add(-25 * time.Hour), AttemptedAt: now.Add(-10 * time.Minute)}, false},
		{"stale, and the last start was 2 hours ago", &state{CheckedAt: now.Add(-25 * time.Hour), AttemptedAt: now.Add(-2 * time.Hour)}, true},
	} {
		c := checker(t, "http://unused.invalid", "0.1.6")
		if tc.state != nil {
			writeState(t, c.StatePath, *tc.state)
		}
		if got := c.Due(); got != tc.want {
			t.Errorf("%s: Due = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMarkAttemptHoldsOffTheNextCheckButKeepsTheAnswer(t *testing.T) {
	c := checker(t, "http://unused.invalid", "0.1.6")
	writeState(t, c.StatePath, state{CheckedAt: now.Add(-25 * time.Hour), Latest: "v0.1.7"})

	c.MarkAttempt()

	if c.Due() {
		t.Error("Due = true straight after MarkAttempt, want false")
	}
	if got, ok := c.Newer(); !ok || got != (Version{0, 1, 7}) {
		t.Errorf("Newer = %v, %v; want the saved v0.1.7 kept", got, ok)
	}
}

func TestRefreshSavesTheLatestRelease(t *testing.T) {
	srv := httptest.NewServer(redirectTo("/releases/tag/v0.1.9"))
	defer srv.Close()
	c := checker(t, srv.URL, "0.1.6")
	attempted := now.Add(-time.Minute)
	writeState(t, c.StatePath, state{CheckedAt: now.Add(-25 * time.Hour), AttemptedAt: attempted, Latest: "v0.1.7"})

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	st := readState(t, c.StatePath)
	if st.Latest != "v0.1.9" || !st.CheckedAt.Equal(now) || !st.AttemptedAt.Equal(attempted) {
		t.Errorf("state = %+v, want latest v0.1.9, checked at %v, attempt mark kept at %v", st, now, attempted)
	}
	if got, ok := c.Newer(); !ok || got != (Version{0, 1, 9}) {
		t.Errorf("Newer = %v, %v; want v0.1.9, true", got, ok)
	}
}

// A failed refresh saves nothing, so the last good answer is still what the
// operator is told, and the attempt mark alone decides when to try again.
func TestRefreshFailureKeepsTheLastAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := checker(t, srv.URL, "0.1.6")
	saved := state{CheckedAt: now.Add(-25 * time.Hour), Latest: "v0.1.7"}
	writeState(t, c.StatePath, saved)

	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh succeeded against a 502, want an error")
	}

	if st := readState(t, c.StatePath); st.Latest != "v0.1.7" || !st.CheckedAt.Equal(saved.CheckedAt) {
		t.Errorf("state = %+v, want it unchanged from %+v", st, saved)
	}
}
