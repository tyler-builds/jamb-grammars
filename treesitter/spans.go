package treesitter

import "github.com/tyler-builds/jamb/codeedit/highlight"

// shift returns the spans moved by the edit e. A span that lies inside the
// replaced range vanishes. A span that starts or ends inside it is cut at the
// start of the edit. The input slice is not modified.
func shift(spans []highlight.Span, e highlight.EditRecord) []highlight.Span {
	delta := len(e.Text) - (e.OldEnd - e.Start)
	out := make([]highlight.Span, 0, len(spans))
	for _, sp := range spans {
		sp.Start = moved(sp.Start, e, delta)
		sp.End = moved(sp.End, e, delta)
		if sp.Start < sp.End {
			out = append(out, sp)
		}
	}
	return out
}

// moved maps the position p through the edit e, whose length change is delta. A
// position at or before the edit's start stays where it is. A position at or
// after its old end moves by delta. A position inside the replaced range moves
// to the start of the edit.
func moved(p int, e highlight.EditRecord, delta int) int {
	switch {
	case p <= e.Start:
		return p
	case p >= e.OldEnd:
		return p + delta
	default:
		return e.Start
	}
}
