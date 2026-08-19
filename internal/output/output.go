// Package output renders results for two audiences from one call site: a
// person reading a terminal, and a script reading stdout.
//
// The discipline is absolute and worth stating once: stdout carries ONLY the
// table or the JSON. Every diagnostic, warning, and error line goes to stderr.
// That is what lets `basa ... --json | jq` keep working even when the command
// fails — the failure is still valid JSON on stdout, and the human explanation
// is on stderr where jq never sees it.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Writer renders to a stdout/stderr pair. Both are injectable so tests can
// assert on each stream separately, which is the only way to catch a
// diagnostic leaking into stdout.
type Writer struct {
	Out  io.Writer
	Err  io.Writer
	JSON bool
}

func New(out, err io.Writer, asJSON bool) *Writer {
	return &Writer{Out: out, Err: err, JSON: asJSON}
}

// Table is a rendered result: headers plus rows, or key/value pairs for a
// single record.
type Table struct {
	Headers []string
	Rows    [][]string
}

// Record renders a single item as aligned key/value lines rather than a wide
// one-row table, which is unreadable past about four columns.
type Record struct {
	Fields []Field
}

type Field struct {
	Key   string
	Value string
}

// Data emits a successful result. In JSON mode the raw payload is written
// verbatim so the shape is the API's, not a CLI reinterpretation of it.
func (w *Writer) Data(payload any, human func(io.Writer) error) error {
	if w.JSON {
		enc := json.NewEncoder(w.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}
	return human(w.Out)
}

// Record writes aligned key/value lines.
func (w *Writer) Record(r Record) error {
	tw := tabwriter.NewWriter(w.Out, 0, 2, 2, ' ', 0)
	for _, f := range r.Fields {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", f.Key, f.Value); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// Table writes a header row and its rows.
func (w *Writer) Table(t Table) error {
	tw := tabwriter.NewWriter(w.Out, 0, 2, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, strings.Join(t.Headers, "\t")); err != nil {
		return err
	}
	for _, row := range t.Rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// Error reports a failure. The human sentence and its hint go to stderr; in
// JSON mode a machine-readable object also goes to stdout so a piped consumer
// gets structured output on the failure path rather than an empty stream.
func (w *Writer) Error(msg, hint string) {
	fmt.Fprintln(w.Err, msg)
	if hint != "" {
		fmt.Fprintln(w.Err, hint)
	}

	if w.JSON {
		enc := json.NewEncoder(w.Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"error": map[string]string{"message": msg, "hint": hint},
		})
	}
}

// Notice is for information the operator should see but a script must never
// parse — which environment is active, a keyring fallback warning. Always
// stderr, never stdout.
func (w *Writer) Notice(format string, args ...any) {
	fmt.Fprintf(w.Err, format+"\n", args...)
}
