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

// WriteTo prints every licence text, separated so each component's notice is
// unambiguously its own.
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

	out := &errWriter{w: w}

	out.printf("basa links the following third-party components.\n")
	out.printf("Each notice below is a verbatim copy of the licence as distributed.\n")

	for _, name := range names {
		body, err := texts.ReadFile(name)
		if err != nil {
			return err
		}

		// body is an argument, never the format: a licence text containing a
		// percent sign must not be interpreted as a verb.
		out.printf("\n%s\n%s\n\n%s", strings.Repeat("=", 78), name, body)
		if !strings.HasSuffix(string(body), "\n") {
			out.printf("\n")
		}
	}

	return out.err
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
