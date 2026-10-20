package treesitter

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	grammars "github.com/tyler-builds/jamb-grammars"
	wasmts "github.com/tyler-builds/jamb-grammars/internal/treesitter"
	"github.com/tyler-builds/jamb/codeedit/highlight"
)

// DefaultMaxBytes is the largest document a Session highlights when
// Options.MaxBytes is zero.
const DefaultMaxBytes = 5 << 20

// margin is how far beyond the visible range the worker computes spans, in
// bytes, so that a short scroll shows colors at once.
const margin = 4096

// refreshDelay is how long the text must be idle before the worker replaces an
// incremental tree that has syntax errors with a fresh parse. A fresh parse of a
// large file takes about a second, so it waits for a pause in typing.
const refreshDelay = 250 * time.Millisecond

// refreshLimit is the largest document whose tree is refreshed. A fresh parse
// takes about half a millisecond per kilobyte, so above this size it would hold
// up the colors of the next edits for too long, and the incremental tree is kept.
const refreshLimit = 256 << 10

// ErrTooLarge is returned by NewSession for a document over the size limit. Show
// such a document without a highlighter.
var ErrTooLarge = errors.New("treesitter: document is larger than the highlighting limit")

// ErrClosed is returned by Edit and SetViewport after Close.
var ErrClosed = errors.New("treesitter: session is closed")

// Options configures a Session.
type Options struct {
	// MaxBytes is the largest document that is highlighted. Zero means
	// DefaultMaxBytes, and a negative value means no limit. A larger document
	// gets ErrTooLarge from NewSession.
	MaxBytes int

	// Notify is called from the session's worker goroutine each time new spans
	// are published. It must not block, and it must not call Close on the
	// session, which waits for the worker. A typical Notify requests a repaint
	// on the UI thread, such as ctx.Post(ctx.Invalidate) in a jamb component
	// (see the package example).
	Notify func()
}

// Session highlights one document. Its methods are safe for concurrent use,
// although Edit and SetViewport are meant for the typing thread.
type Session struct {
	inst   *wasmts.Instance // owned by the worker from NewSession on
	names  []string
	notify func()
	wake   chan struct{} // capacity one: a pending wake-up for the worker
	quit   chan struct{}
	done   chan struct{}

	mu        sync.Mutex
	closed    bool
	err       error                  // the first error from the worker; highlighting stops
	length    int                    // document length, as the typing thread sees it
	version   uint64                 // the number of edits accepted
	synced    uint64                 // the version of the last spans the worker published
	rev       uint64                 // changes whenever spans does: at each accepted edit and each publish
	spans     []highlight.Span       // the spans to paint; replaced, never modified in place
	queue     []highlight.EditRecord // edits the worker has not applied yet; Text is owned here
	first     bool                   // the worker has not parsed the document yet
	view      [2]int                 // the range the worker computes spans for, with margin
	viewDirty bool                   // view changed since the worker last computed spans
	busy      bool                   // the worker has taken a round whose result is not published yet
	refresh   bool                   // the last incremental parse left errors; a fresh parse follows once the text is idle
	lastEdit  time.Time              // when Edit last accepted a change
	timer     *time.Timer            // wakes the worker when a pending refresh becomes due
	reparses  atomic.Int64           // the number of parses the worker has run
}

// workHook, when set, runs in the worker before each round. Tests use it to hold
// the worker, so that they can check what the typing thread sees in between.
var workHook func()

// NewSession opens a document with the grammar g, such as golang.Grammar, and
// starts its worker. text must be the document's text as the editor has it when
// the session is given to it. The worker parses the text and publishes the first
// spans in the background. NewSession returns ErrTooLarge for a document over
// Options.MaxBytes, and an error when the grammar's module or query does not
// load.
func NewSession(g *grammars.Grammar, text []byte, opts Options) (*Session, error) {
	if g == nil {
		return nil, errors.New("treesitter: nil grammar")
	}
	limit := opts.MaxBytes
	if limit == 0 {
		limit = DefaultMaxBytes
	}
	if limit > 0 && len(text) > limit {
		return nil, ErrTooLarge
	}
	mod, err := wasmts.Compile(g.Wasm())
	if err != nil {
		return nil, fmt.Errorf("treesitter: %s: %w", g.Name(), err)
	}
	inst, err := mod.Instantiate()
	if err != nil {
		return nil, fmt.Errorf("treesitter: %s: %w", g.Name(), err)
	}
	if err := inst.Init(text); err != nil {
		inst.Close()
		return nil, fmt.Errorf("treesitter: %s: %w", g.Name(), err)
	}
	if err := inst.SetQuery(g.Query()); err != nil {
		inst.Close()
		return nil, fmt.Errorf("treesitter: %s: %w", g.Name(), err)
	}
	names, err := inst.CaptureNames()
	if err != nil {
		inst.Close()
		return nil, fmt.Errorf("treesitter: %s: %w", g.Name(), err)
	}
	s := &Session{
		inst:      inst,
		names:     names,
		notify:    opts.Notify,
		wake:      make(chan struct{}, 1),
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
		length:    len(text),
		first:     true,
		view:      [2]int{0, min(len(text), margin)},
		viewDirty: true,
	}
	s.timer = time.AfterFunc(refreshDelay, s.signal)
	s.timer.Stop()
	s.signal()
	go s.run()
	return s, nil
}

// Edit applies a change to the document. It never waits for the worker: the
// existing spans are shifted at once, and the change is queued for the worker,
// which applies every queued change before one reparse. Edit returns an error,
// without changing anything, when the range is not inside the document.
func (s *Session) Edit(e highlight.EditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if s.err != nil {
		return s.err
	}
	if e.Start < 0 || e.OldEnd < e.Start || e.OldEnd > s.length {
		return fmt.Errorf("treesitter: edit [%d, %d) is outside the document of %d bytes", e.Start, e.OldEnd, s.length)
	}
	e.Text = slices.Clone(e.Text)
	s.version++
	s.rev++
	s.lastEdit = time.Now()
	s.spans = shift(s.spans, e)
	s.length += len(e.Text) - (e.OldEnd - e.Start)
	s.queue = append(s.queue, e)
	s.signal()
	return nil
}

// SetViewport tells the session which bytes are visible, [lo, hi). The worker
// computes spans for that range with a margin on each side. Until it publishes
// them, Spans shows the old spans, shifted by the edits. The cost of a round
// grows with the range, so pass the visible lines, not the whole document: spans
// for half of a 2.5 MB Go file take about a second.
func (s *Session) SetViewport(lo, hi int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if lo < 0 || hi < lo {
		return fmt.Errorf("treesitter: viewport [%d, %d) is not a range", lo, hi)
	}
	lo = max(0, min(lo-margin, s.length))
	hi = min(s.length, hi+margin)
	if [2]int{lo, hi} == s.view {
		return nil
	}
	s.view = [2]int{lo, hi}
	s.viewDirty = true
	s.signal()
	return nil
}

// Spans returns the spans to paint and their revision. The revision changes
// whenever the spans do: when an edit shifts them, and when the worker publishes
// new ones, so a caller that keeps an index of the spans rebuilds it when the
// revision differs. The slice must not be modified. It is replaced, not changed,
// so a slice held by the caller stays valid.
func (s *Session) Spans() ([]highlight.Span, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spans, s.rev
}

// Synced returns the number of edits that the published spans reflect. Until it
// reaches the number of edits made, the spans of the latest edits are the earlier
// spans shifted into place.
func (s *Session) Synced() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.synced
}

// CaptureNames returns the capture names of the grammar's highlight query,
// indexed by highlight.Span.Capture. The editor maps them to the theme's syntax
// styles with highlight.Closest.
func (s *Session) CaptureNames() []string {
	return s.names
}

// Err returns the error that stopped highlighting, or nil. After an error the
// session keeps accepting calls, but it publishes no more spans.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close stops the worker and releases the document's instance. It waits for the
// worker to finish its current step, which takes a few milliseconds at most.
// Calls after Close return ErrClosed. Close is safe to call more than once.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.timer.Stop()
	close(s.quit)
	s.mu.Unlock()
	<-s.done
	return nil
}

// signal wakes the worker. It never blocks: one pending wake-up is enough,
// because the worker takes everything queued each time it runs.
func (s *Session) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// run is the worker goroutine. It owns the instance and closes it on exit.
func (s *Session) run() {
	defer close(s.done)
	defer s.inst.Close()
	for {
		select {
		case <-s.quit:
			return
		case <-s.wake:
		}
		s.work()
	}
}

// work applies the queued edits, reparses once if needed, computes the spans of
// the viewport and publishes them. Edits that arrive during the work stay
// queued, and they shift the published spans before the worker's result is
// swapped in.
func (s *Session) work() {
	if workHook != nil {
		workHook()
	}
	s.mu.Lock()
	if s.closed || s.err != nil {
		s.mu.Unlock()
		return
	}
	edits := s.queue
	s.queue = nil
	// A refresh waits until the text has been idle for refreshDelay, so that
	// typing is not held up by a full parse.
	refresh := false
	if s.refresh && len(edits) == 0 {
		if idle := time.Since(s.lastEdit); idle >= refreshDelay {
			refresh = true
		} else {
			s.timer.Reset(refreshDelay - idle)
		}
	}
	parse, full := len(edits) > 0 || s.first || refresh, s.first || refresh
	dirty := s.viewDirty
	s.viewDirty = false
	view := s.view
	if !parse && !dirty {
		s.mu.Unlock()
		return
	}
	s.busy = true
	s.mu.Unlock()

	err := s.compute(edits, parse, full, view)

	s.mu.Lock()
	s.busy = false
	if s.closed {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.err = err
		s.spans = nil
	} else if s.refresh {
		s.signal() // arm the timer for the refresh in the next round
	}
	s.mu.Unlock()
	if s.notify != nil {
		s.notify()
	}
}

// compute runs the instance's steps for one round and publishes the result. It
// is called only from the worker.
func (s *Session) compute(edits []highlight.EditRecord, parse, full bool, view [2]int) error {
	for _, e := range edits {
		if err := s.inst.Edit(e.Start, e.OldEnd, e.Text); err != nil {
			return fmt.Errorf("treesitter: apply edit: %w", err)
		}
	}
	refresh := false
	if parse {
		if err := s.inst.Parse(full); err != nil {
			return fmt.Errorf("treesitter: parse: %w", err)
		}
		s.reparses.Add(1)
		if !full {
			// Incremental parsing can give a different tree from a fresh parse
			// while the text has syntax errors. Such a tree is replaced by a
			// fresh parse when the worker is idle.
			hasErr, err := s.inst.TreeHasError()
			if err != nil {
				return fmt.Errorf("treesitter: parse: %w", err)
			}
			refresh = hasErr
		}
	}
	raw, err := s.inst.Spans(view[0], view[1], nil)
	if err != nil {
		return fmt.Errorf("treesitter: spans: %w", err)
	}
	spans := make([]highlight.Span, 0, len(raw)/3)
	for i := 0; i+2 < len(raw); i += 3 {
		spans = append(spans, highlight.Span{Start: int(raw[i]), End: int(raw[i+1]), Capture: int(raw[i+2])})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.first = false
		s.refresh = refresh && s.length <= refreshLimit
		// Spans are for the version the worker started with. Edits made since
		// then are still in the queue, and they shift the spans into place.
		for _, e := range s.queue {
			spans = shift(spans, e)
		}
		s.spans = spans
		s.synced = s.version
		s.rev++
	}
	return nil
}
