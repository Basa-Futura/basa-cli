// Package licenses carries the verbatim licence text of every third-party
// component linked into the binary.
//
// This exists to discharge an obligation rather than to document one. MIT and
// BSD require their permission and warranty notices to accompany "copies or
// substantial portions" of the software, and a statically linked Go binary is
// such a copy. A table of copyright lines in a repository file does not travel
// with a downloaded binary, so the texts are embedded and `basa licenses`
// prints them — the notices are then inside the artefact that carries the code.
//
// Linux- and Windows-only components are included in every build's embedded
// set on purpose. Which keyring backend links is decided at compile time, but
// the cost of carrying nine short files is a few kilobytes, and a notice that
// is present on the wrong platform is harmless where a missing one is not.
//
// Because that policy makes the set a union rather than a per-platform
// resolution, the set is pinned against go.mod by a test. It has to be: a
// Windows-only dependency was missing from this directory until the test
// existed, and nothing in a build or a run would have told anyone.
package licenses

import (
	"embed"
	"fmt"
	"io"
	"runtime/debug"
	"sort"
	"strings"
)

//go:embed *.txt
var texts embed.FS

// mainModule is this repository, which needs no third-party notice.
const mainModule = "github.com/Basa-Futura/basa-cli"

// All returns the file name of every embedded licence text, sorted so the output
// is stable between runs. The texts themselves are read by WriteTo.
func All() ([]string, error) {
	entries, err := texts.ReadDir(".")
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	return names, nil
}

// WriteTo prints every licence text, each under the module path and version it
// belongs to, separated so no notice can be mistaken for part of another.
//
// The module and version come from the binary's own build information, not from
// a header inside the text file. The files used to carry one — a line naming
// the module and version above the licence — which made them not quite the
// verbatim copies the policy claims, and left the version to be updated by hand
// on every upgrade. Build information cannot drift from what was linked.
//
// Write failures are reported rather than dropped, which matters more here than
// in ordinary output code. This function is *how* the notices travel with a
// distributed binary, so a run that writes half of them and returns nil would
// report an obligation discharged that was not. The caller exits non-zero on it.
func WriteTo(w io.Writer) error {
	names, err := All()
	if err != nil {
		return err
	}

	versions, known := linkedVersions()
	out := &errWriter{w: w}

	out.printf("basa links the following third-party components.\n")
	out.printf("Each notice below is a byte-for-byte copy of the licence file distributed with the module.\n")

	for _, name := range names {
		body, err := texts.ReadFile(name)
		if err != nil {
			return err
		}

		out.printf("\n%s\n", strings.Repeat("=", 78))
		switch mv, ok := versions[name]; {
		case ok:
			out.printf("%s\n", mv)
		case known:
			// Build information lists only what is linked into this binary, so
			// a notice carried for another platform has no version to show.
			// Say so, rather than leave a gap that reads like an omission.
			out.printf("(not linked into this build — carried so that no build ships without its notice)\n")
		}
		// body is an argument, never the format: a licence text containing a
		// percent sign must not be interpreted as a verb.
		out.printf("%s\n\n%s", name, body)
		if !strings.HasSuffix(string(body), "\n") {
			out.printf("\n")
		}
	}

	return out.err
}

// linkedVersions maps each notice file to "module version", read from the build
// information Go records in every binary it produces.
//
// Two things about that information shape what WriteTo can say. It lists only
// the modules linked into *this* binary, so a Windows-only dependency has no
// entry in a macOS build — the notice is still embedded, and WriteTo marks it
// as carried rather than linked. And a go-test binary records no dependencies
// at all, so the second return distinguishes "nothing known" from "known, and
// this one is not linked": the first prints nothing, the second the note.
func linkedVersions() (versions map[string]string, known bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok || len(info.Deps) == 0 {
		return nil, false
	}

	versions = make(map[string]string, len(info.Deps))
	for _, d := range info.Deps {
		if d.Replace != nil {
			d = d.Replace
		}
		versions[noticeFile(d.Path)] = d.Path + " " + d.Version
	}
	return versions, true
}

// noticeFile maps a module path onto the licence file that carries its notice:
// <owner>-<repo>.txt, with golang.org/x/... keeping a golang prefix so the files
// sort together and read unambiguously. Shared by WriteTo and by the tests that
// pin the embedded set, so the two cannot disagree about the mapping.
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

// errWriter latches the first write failure so a run of writes needs one check
// at the end instead of one after every line. After a failure it stops writing,
// so no caller can be handed a partial notice together with a nil error.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}
