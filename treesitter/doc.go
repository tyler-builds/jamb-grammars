// Package treesitter highlights code with the tree-sitter grammars of this
// repository. It is an optional highlighter for jamb's code editor
// (github.com/tyler-builds/jamb/codeedit), for an app that wants the accuracy of
// a parse tree; the editor's built-in highlighter needs no grammars.
//
// A Session holds one open document and satisfies codeedit.Highlighter, so an app
// gives it to an editor with EditorElement.Highlighter. The editor calls Edit for
// every change and SetViewport when the visible rows move, and paints with what
// Spans returns. Edit returns at once: it shifts the spans it has, so colors stay
// in place, and queues the change for a background worker. The worker owns the
// document's WebAssembly instance. It applies every queued change, reparses once,
// computes the spans of the visible range, and publishes them. Options.Notify
// tells the app when new spans arrive, so that it can request a frame.
//
// The editor neither starts nor closes a highlighter that the app gives it. The
// app makes the session from the document's text, closes it when the editor goes
// away, and makes a new one when the editor shows another document. Give each
// editor its own session: every editor sends the changes it sees to its
// highlighter, so two editors of one document would send each change twice. In a
// jamb component:
//
//	func codeView(doc *codeedit.Document) ui.Element {
//		return ui.Component(func(ctx *ui.Context) ui.Element {
//			hl := ui.Local(ctx, "highlighter", &highlighter{}).Peek()
//			if hl.s == nil && hl.err == nil {
//				hl.s, hl.err = treesitter.NewSession(golang.Grammar, []byte(doc.Text()), treesitter.Options{
//					// Notify runs on the session's worker: render again on the UI thread.
//					Notify: func() { ctx.Post(ctx.Invalidate) },
//				})
//				if hl.err == nil {
//					ctx.OnDispose(func() { hl.s.Close() })
//				}
//			}
//			ed := codeedit.Editor(doc)
//			if hl.s != nil { // nil after an error, such as ErrTooLarge: plain text
//				ed.Highlighter(hl.s)
//			}
//			return ed
//		})
//	}
//
//	// highlighter is the session of one editor, or why it could not start.
//	type highlighter struct {
//		s   *treesitter.Session
//		err error
//	}
//
// The modules are compiled once per process, and the compiled code is kept in
// the jamb-grammars/wasm directory under os.UserCacheDir, or in memory when that
// directory cannot be used.
package treesitter
