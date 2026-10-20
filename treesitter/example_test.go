package treesitter_test

import (
	"github.com/tyler-builds/jamb-grammars/golang"
	"github.com/tyler-builds/jamb-grammars/treesitter"
	"github.com/tyler-builds/jamb/codeedit"
	"github.com/tyler-builds/jamb/ui"
)

// codeView is an editor of doc that highlights it with a session of its own.
func codeView(doc *codeedit.Document) ui.Element {
	return ui.Component(func(ctx *ui.Context) ui.Element {
		hl := ui.Local(ctx, "highlighter", &highlighter{}).Peek()
		if hl.s == nil && hl.err == nil {
			hl.s, hl.err = treesitter.NewSession(golang.Grammar, []byte(doc.Text()), treesitter.Options{
				// Notify runs on the session's worker: render again on the UI thread.
				Notify: func() { ctx.Post(ctx.Invalidate) },
			})
			if hl.err == nil {
				ctx.OnDispose(func() { hl.s.Close() })
			}
		}
		ed := codeedit.Editor(doc)
		if hl.s != nil { // nil after an error, such as ErrTooLarge: plain text
			ed.Highlighter(hl.s)
		}
		return ed
	})
}

// highlighter is the session of one editor, or why it could not start.
type highlighter struct {
	s   *treesitter.Session
	err error
}

// An app gives the editor a session that it makes from the document's text, and
// closes the session when the editor goes away.
func Example() {
	doc := codeedit.NewDocument("package main\n\nfunc main() {}\n")
	root := ui.Div().Child(codeView(doc))
	_ = root // the app's root element, as app.Run shows it
}
