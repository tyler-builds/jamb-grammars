package treesitter_test

import (
	"slices"
	"testing"

	"github.com/tyler-builds/jamb-grammars/golang"
	"github.com/tyler-builds/jamb-grammars/treesitter"
	"github.com/tyler-builds/jamb/codeedit"
	"github.com/tyler-builds/jamb/codeedit/highlight"
	"github.com/tyler-builds/jamb/ui"
	"github.com/tyler-builds/jamb/ui/uitest"
)

// recorder is a session that records what the editor calls. The editor calls it
// on the UI thread only, which is the test's goroutine.
type recorder struct {
	*treesitter.Session
	edits     []highlight.EditRecord
	viewports int
	painted   uint64 // the revision of the spans the editor read last
	reads     int
}

func (r *recorder) Edit(e highlight.EditRecord) error {
	r.edits = append(r.edits, highlight.EditRecord{Start: e.Start, OldEnd: e.OldEnd, Text: slices.Clone(e.Text)})
	return r.Session.Edit(e)
}

func (r *recorder) SetViewport(lo, hi int) error {
	r.viewports++
	return r.Session.SetViewport(lo, hi)
}

func (r *recorder) Spans() ([]highlight.Span, uint64) {
	spans, rev := r.Session.Spans()
	r.painted, r.reads = rev, r.reads+1
	return spans, rev
}

// TestEditorPaintsPublishedSpans wires a session to an editor as the package
// example does, headlessly, and checks that the editor sends it the viewport and
// each change, and paints again with the spans the worker publishes: Notify's
// ctx.Post(ctx.Invalidate) is what makes it paint, because an idle frame paints
// nothing.
func TestEditorPaintsPublishedSpans(t *testing.T) {
	doc := codeedit.NewDocument("package main\n\nfunc main() {}\n")
	rec := &recorder{}
	u := uitest.New(t, func(ctx *ui.Context) ui.Element {
		if rec.Session == nil {
			s, err := treesitter.NewSession(golang.Grammar, []byte(doc.Text()), treesitter.Options{
				Notify: func() { ctx.Post(ctx.Invalidate) },
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			rec.Session = s
		}
		return codeedit.Editor(doc).Highlighter(rec)
	})
	// current reports whether the editor last painted the latest spans, and
	// they reflect every edit.
	current := func(edits uint64) func() bool {
		return func() bool {
			spans, rev := rec.Session.Spans()
			return len(spans) > 0 && rec.Session.Synced() == edits && rec.painted == rev
		}
	}
	u.WaitFor("a paint with the first spans", current(0))
	if rec.viewports == 0 {
		t.Fatal("the editor sent no viewport")
	}

	if err := doc.Replace(0, 0, "// c\n"); err != nil {
		t.Fatal(err)
	}
	u.Frame()
	want := []highlight.EditRecord{{Start: 0, OldEnd: 0, Text: []byte("// c\n")}}
	if !slices.EqualFunc(rec.edits, want, func(a, b highlight.EditRecord) bool {
		return a.Start == b.Start && a.OldEnd == b.OldEnd && string(a.Text) == string(b.Text)
	}) {
		t.Fatalf("edits = %v, want %v", rec.edits, want)
	}
	u.WaitFor("a paint with the spans of the edit", current(1))
	spans, _ := rec.Session.Spans()
	names := rec.Session.CaptureNames()
	text := doc.Text()
	if !slices.ContainsFunc(spans, func(sp highlight.Span) bool {
		return text[sp.Start:sp.End] == "// c" && names[sp.Capture] == "comment"
	}) {
		t.Fatal("the inserted comment has no comment span")
	}
	if err := rec.Session.Err(); err != nil {
		t.Fatal(err)
	}
}
