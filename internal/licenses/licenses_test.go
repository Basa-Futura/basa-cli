package licenses

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestEveryDependencyHasANotice pins the embedded set against go.mod, in both
// directions: a dependency with no licence text, and a licence text belonging to
// no dependency.
//
// This test is the point of the package. The notices were maintained by hand and
// silently lost github.com/inconshreveable/mousetrap — Windows-only, reached
// through cobra, Apache-2.0. Nothing in a build, a test, or a run would have
// surfaced that; the omission was invisible until someone re-derived the list.
// A table can drift, and a regeneration recipe only works when someone runs it.
//
// It reads go.mod rather than resolving `go list -deps` per platform because the
// policy in THIRD-PARTY-NOTICES.md is to embed every notice in every build. That
// makes the union of all platforms the right question, and go.mod is the union —
// which also keeps this test hermetic, with no go tool and no network.
func TestEveryDependencyHasANotice(t *testing.T) {
	want := map[string]string{} // notice file -> module it belongs to
	for _, mod := range requiredModules(t) {
		want[noticeFile(mod)] = mod
	}

	embedded, err := All()
	if err != nil {
		t.Fatalf("All(): %v", err)
	}
	got := map[string]bool{}
	for _, name := range embedded {
		got[name] = true
	}

	for name, mod := range want {
		if !got[name] {
			t.Errorf("%s is required by go.mod but has no licence text: add internal/licenses/%s", mod, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("internal/licenses/%s belongs to no module in go.mod — stale, or misnamed", name)
		}
	}
}

// An empty or truncated file would satisfy the set comparison above while
// carrying no notice at all — the same silent-zero shape the omission had.
func TestEveryNoticeCarriesItsText(t *testing.T) {
	names, err := All()
	if err != nil {
		t.Fatalf("All(): %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no licence texts are embedded at all")
	}

	for _, name := range names {
		body, err := texts.ReadFile(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(body) < 500 {
			t.Errorf("%s is %d bytes — too short to be a licence text", name, len(body))
		}
		if !strings.Contains(string(body), "WARRANT") && !strings.Contains(string(body), "warrant") {
			t.Errorf("%s carries no warranty text, which is the half a copyright line does not satisfy", name)
		}
	}
}

// requiredModules returns every module path go.mod requires, direct and
// indirect. Parsed by hand rather than with golang.org/x/mod: adding a
// dependency in order to police dependency notices would be its own joke, and
// the format needed here is two shapes wide.
func requiredModules(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	var mods []string
	inBlock := false

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i != -1 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}

		switch {
		case line == "require (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			if path := strings.Fields(line)[0]; path != mainModule {
				mods = append(mods, path)
			}
		case strings.HasPrefix(line, "require "):
			if fields := strings.Fields(line); len(fields) > 1 && fields[1] != mainModule {
				mods = append(mods, fields[1])
			}
		}
	}

	if len(mods) == 0 {
		t.Fatal("parsed no modules out of go.mod — the parser is wrong, not the file")
	}
	return mods
}

// failAfter writes happily up to limit bytes and then fails, standing in for a
// full disk or a writer closed under us.
type failAfter struct {
	written int
	limit   int
}

func (f *failAfter) Write(p []byte) (int, error) {
	if f.written >= f.limit {
		return 0, errors.New("no space left on device")
	}
	f.written += len(p)
	return len(p), nil
}

// A truncated run must not report success. WriteTo is how the notices reach
// someone who downloaded a binary, so "wrote half of them and exited 0" would
// claim an obligation was discharged when it was not.
func TestWriteToReportsWriteFailures(t *testing.T) {
	for name, limit := range map[string]int{
		"fails immediately":     0,
		"fails after a header":  120,
		"fails mid-way through": 5000,
	} {
		t.Run(name, func(t *testing.T) {
			if err := WriteTo(&failAfter{limit: limit}); err == nil {
				t.Error("want an error from a failing writer, got nil")
			}
		})
	}
}

// The good path still has to emit every notice — an error-latching writer that
// quietly stopped early would satisfy the test above and break the obligation.
func TestWriteToEmitsEveryNotice(t *testing.T) {
	var buf bytes.Buffer

	if err := WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	names, err := All()
	if err != nil {
		t.Fatalf("All(): %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no notices embedded")
	}

	for _, name := range names {
		if !strings.Contains(buf.String(), name) {
			t.Errorf("output is missing the notice for %s", name)
		}
	}

	// Each licence body, not just its file name, has to be in there.
	for _, name := range names {
		body, err := texts.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Contains(buf.Bytes(), body) {
			t.Errorf("%s is named but its text is not reproduced verbatim", name)
		}
	}
}

// Every embedded text must be byte-for-byte the licence file its module ships.
// Not "contains the licence" — identical. Eight of these files once carried a
// hand-written header naming the module and version above the text, which made
// them handy to read and slightly untrue to the claim that they are verbatim.
// The module and version now come from build information instead.
//
// This is the one test in the package that needs the go tool: identity can only
// be checked against the source, and `go list -m` is how the source is found.
// go test already needs the module cache to compile, so nothing new is asked —
// and there is deliberately no skip. A guard that steps aside when it cannot
// look is the silent zero this package exists to prevent.
func TestEveryNoticeIsVerbatim(t *testing.T) {
	for _, mod := range requiredModules(t) {
		name := noticeFile(mod)
		embedded, err := texts.ReadFile(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}

		src, srcName := licenceFileIn(t, moduleDir(t, mod))
		if !bytes.Equal(embedded, src) {
			t.Errorf("%s is not byte-identical to %s's %s (embedded %d bytes, upstream %d)",
				name, mod, srcName, len(embedded), len(src))
		}
	}
}

func moduleDir(t *testing.T, mod string) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", mod).Output()
	if err != nil {
		t.Fatalf("go list -m %s: %v", mod, err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatalf("go list -m %s: no directory — not in the module cache?", mod)
	}
	return dir
}

// licenceFileIn finds the licence file a module ships. The names vary —
// basecamp/cli calls its MIT-LICENSE — which is exactly the sweep that once
// reported that module as having no licence at all.
func licenceFileIn(t *testing.T, dir string) ([]byte, string) {
	t.Helper()
	for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "MIT-LICENSE", "COPYING"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return b, name
		}
	}
	t.Fatalf("no licence file found in %s", dir)
	return nil, ""
}

// The built binary names each linked module with the version that was linked,
// read from its own build information. This builds and runs it because a
// go-test binary records no dependencies at all — a harness test can never see
// these lines, and the first attempt at this check learned that the hard way.
//
// Build information is per-artefact truth: a module not linked on this platform
// has no version here, and the notice carried for it says so instead. The
// expected split therefore depends on runtime.GOOS.
func TestBuiltBinaryReportsLinkedVersions(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "basa")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/basa").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "licenses").Output()
	if err != nil {
		t.Fatalf("basa licenses: %v", err)
	}
	text := string(out)

	// Linked on every platform: a version line for each, no exceptions.
	for _, mod := range []string{"github.com/basecamp/cli", "github.com/spf13/cobra", "github.com/spf13/pflag",
		"github.com/zalando/go-keyring", "golang.org/x/sys", "golang.org/x/term"} {
		if !strings.Contains(text, "\n"+mod+" v") {
			t.Errorf("no version line for %s", mod)
		}
	}

	// Platform-specific: a version where linked, the carried-not-linked note
	// everywhere else, and never both or neither.
	platformOnly := map[string]string{
		"github.com/godbus/dbus/v5":            "linux",
		"github.com/danieljoos/wincred":        "windows",
		"github.com/inconshreveable/mousetrap": "windows",
	}
	wantNotes := 0
	for mod, goos := range platformOnly {
		linked := goos == runtime.GOOS
		if linked != strings.Contains(text, "\n"+mod+" v") {
			t.Errorf("%s: linked-on-%s=%v but version line present=%v", mod, runtime.GOOS, linked, !linked)
		}
		if !linked {
			wantNotes++
		}
	}
	if got := strings.Count(text, "(not linked into this build"); got != wantNotes {
		t.Errorf("carried-not-linked notes: got %d, want %d on %s", got, wantNotes, runtime.GOOS)
	}
}
