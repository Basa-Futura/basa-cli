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
	"sort"
	"strings"
)

//go:embed *.txt
var texts embed.FS

// All returns every embedded licence text, keyed by file name, sorted so the
// output is stable between runs.
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

// WriteTo prints every licence text, separated so each component's notice is
// unambiguously its own.
func WriteTo(w io.Writer) error {
	names, err := All()
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "basa links the following third-party components.\n")
	fmt.Fprintf(w, "Each notice below is a verbatim copy of the licence as distributed.\n")

	for _, name := range names {
		body, err := texts.ReadFile(name)
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s\n%s\n\n%s", strings.Repeat("=", 78), name, body)
		if !strings.HasSuffix(string(body), "\n") {
			fmt.Fprintln(w)
		}
	}

	return nil
}
