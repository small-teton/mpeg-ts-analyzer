package tsparser

import "fmt"

// dumpField prints one aligned "<prefix><label> : <value>" line, left-padding
// prefix+label to colonCol so the ':' lines up regardless of label length.
//
// It is the shared formatter for every PSI/header dump (PAT, PMT and its
// descriptor lines, PES, adaptation field, TS header). Each dump wraps it with
// its own prefix and colon column (see patField, pmtField, pesField, afField,
// tsField, descField); keeping the column per-dump lets each table pick a width
// that clears its longest label without over-padding the others.
func dumpField(prefix string, colonCol int, label, format string, args ...interface{}) {
	fmt.Printf("%-*s: %s\n", colonCol, prefix+label, fmt.Sprintf(format, args...))
}
