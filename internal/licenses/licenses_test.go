package licenses

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mainModule is this repository, which needs no third-party notice.
const mainModule = "github.com/Basa-Futura/basa-cli"

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

// noticeFile maps a module path onto the licence file that must carry its
// notice: <owner>-<repo>.txt, with golang.org/x/... keeping a golang prefix so
// the files sort together and read unambiguously.
func noticeFile(module string) string {
	path := module

	// A major-version suffix is not part of the name: godbus/dbus/v5 is dbus.
	if i := strings.LastIndex(path, "/v"); i != -1 {
		if rest := path[i+len("/v"):]; rest != "" && strings.Trim(rest, "0123456789") == "" {
			path = path[:i]
		}
	}

	parts := strings.Split(path, "/")
	switch parts[0] {
	case "github.com":
		parts = parts[1:]
	case "golang.org":
		parts[0] = "golang"
	}

	return strings.Join(parts, "-") + ".txt"
}
