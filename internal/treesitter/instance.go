package treesitter

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Instance is one instance of a module, holding one document. It is not safe
// for concurrent use. After a call returns an error from the module, the
// instance may be left in an unknown state and should be closed.
type Instance struct {
	mod api.Module
	fn  instanceFuncs
	out uint32 // scratch buffer for Spans, and its size in bytes
	cap uint32
}

// instanceFuncs are the shim's exports, looked up once per instance.
type instanceFuncs struct {
	alloc, free                      api.Function
	init, setQuery, queryErrorOffset api.Function
	captureCount, captureName        api.Function
	edit, parse, spans               api.Function
	treeHash, treeHasError           api.Function
}

// Instantiate creates a new instance of the module. The instance starts with no
// document; call Init first.
func (m *Module) Instantiate() (*Instance, error) {
	ctx := context.Background()
	mu.Lock()
	r, err := runtimeLocked()
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	mod, err := r.InstantiateModule(ctx, m.compiled, wazero.NewModuleConfig().WithName("").WithStartFunctions("_initialize"))
	mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("treesitter: instantiate module: %w", err)
	}
	in := &Instance{mod: mod}
	get := func(name string) api.Function {
		f := mod.ExportedFunction(name)
		if f == nil {
			err = errors.Join(err, fmt.Errorf("treesitter: module does not export %s", name))
		}
		return f
	}
	in.fn = instanceFuncs{
		alloc: get("buf_alloc"), free: get("buf_free"),
		init: get("doc_init"), setQuery: get("doc_set_query"), queryErrorOffset: get("doc_query_error_offset"),
		captureCount: get("doc_capture_count"), captureName: get("doc_capture_name"),
		edit: get("doc_edit"), parse: get("doc_parse"), spans: get("doc_spans"),
		treeHash: get("doc_tree_hash"), treeHasError: get("doc_tree_has_error"),
	}
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}
	return in, nil
}

// call invokes f and returns its first result, or zero when it has none.
func (in *Instance) call(f api.Function, args ...uint64) (uint64, error) {
	r, err := f.Call(context.Background(), args...)
	if err != nil {
		return 0, err
	}
	if len(r) == 0 {
		return 0, nil
	}
	return r[0], nil
}

// put copies b into the module's memory and returns its address. Empty input
// returns address zero without allocating.
func (in *Instance) put(b []byte) (uint32, error) {
	if len(b) == 0 {
		return 0, nil
	}
	p, err := in.call(in.fn.alloc, uint64(len(b)))
	if err != nil {
		return 0, err
	}
	if p == 0 {
		return 0, errors.New("treesitter: module is out of memory")
	}
	if !in.mod.Memory().Write(uint32(p), b) {
		return 0, errors.New("treesitter: write outside module memory")
	}
	return uint32(p), nil
}

// release frees memory returned by put. Address zero is ignored.
func (in *Instance) release(p uint32) error {
	if p == 0 {
		return nil
	}
	_, err := in.call(in.fn.free, uint64(p))
	return err
}

// Init sets the document's text and selects the module's grammar. Any earlier
// tree is discarded, so the next Parse must be a full parse.
func (in *Instance) Init(text []byte) error {
	p, err := in.put(text)
	if err != nil {
		return err
	}
	r, err := in.call(in.fn.init, uint64(p), uint64(len(text)))
	if err != nil {
		return err
	}
	if err := in.release(p); err != nil {
		return err
	}
	if int32(r) != 0 {
		return errors.New("treesitter: grammar is not compatible with the runtime")
	}
	return nil
}

// SetQuery compiles the highlight query against the grammar. When the query
// does not match the grammar, the error gives the byte offset of the problem in
// the query text.
func (in *Instance) SetQuery(q []byte) error {
	p, err := in.put(q)
	if err != nil {
		return err
	}
	r, err := in.call(in.fn.setQuery, uint64(p), uint64(len(q)))
	if err != nil {
		return err
	}
	if err := in.release(p); err != nil {
		return err
	}
	if r != 0 {
		off, err := in.call(in.fn.queryErrorOffset)
		if err != nil {
			return err
		}
		return fmt.Errorf("treesitter: highlight query error %d at byte %d", r, off)
	}
	return nil
}

// CaptureNames returns the names of the query's captures, indexed by the
// capture numbers that Spans returns.
func (in *Instance) CaptureNames() ([]string, error) {
	n, err := in.call(in.fn.captureCount)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, n)
	const nameMax = 256
	buf, err := in.call(in.fn.alloc, nameMax)
	if err != nil {
		return nil, err
	}
	if buf == 0 {
		return nil, errors.New("treesitter: module is out of memory")
	}
	defer in.release(uint32(buf))
	for i := uint64(0); i < n; i++ {
		l, err := in.call(in.fn.captureName, i, buf, nameMax)
		if err != nil {
			return nil, err
		}
		if l > nameMax {
			return nil, errors.New("treesitter: capture name longer than 256 bytes")
		}
		b, ok := in.mod.Memory().Read(uint32(buf), uint32(l))
		if !ok {
			return nil, errors.New("treesitter: read outside module memory")
		}
		names = append(names, string(b))
	}
	return names, nil
}

// Edit replaces the bytes from start to oldEnd with text, and edits the tree to
// match. The edit is not reparsed until Parse. It returns an error if the range
// is not inside the document.
func (in *Instance) Edit(start, oldEnd int, text []byte) error {
	p, err := in.put(text)
	if err != nil {
		return err
	}
	r, err := in.call(in.fn.edit, uint64(start), uint64(oldEnd), uint64(p), uint64(len(text)))
	if err != nil {
		return err
	}
	if err := in.release(p); err != nil {
		return err
	}
	if int32(r) != 0 {
		return fmt.Errorf("treesitter: edit [%d, %d) is outside the document", start, oldEnd)
	}
	return nil
}

// Parse parses the document. With full set it parses from scratch, otherwise it
// reuses the tree that the edits have been applied to.
func (in *Instance) Parse(full bool) error {
	f := uint64(0)
	if full {
		f = 1
	}
	r, err := in.call(in.fn.parse, f)
	if err != nil {
		return err
	}
	if int32(r) != 0 {
		return errors.New("treesitter: parser returned no tree")
	}
	return nil
}

// Spans appends to dst the highlight captures that intersect bytes lo to hi, as
// (start, end, capture index) triples in the order the query yields them, and
// returns the extended slice.
func (in *Instance) Spans(lo, hi int, dst []uint32) ([]uint32, error) {
	const want = 3 * 4 * 1024 // room for 1024 captures, grown below when needed
	if in.cap < want {
		if err := in.grow(want); err != nil {
			return nil, err
		}
	}
	n, err := in.call(in.fn.spans, uint64(lo), uint64(hi), uint64(in.out), uint64(in.cap/4/3))
	if err != nil {
		return nil, err
	}
	if n*12 > uint64(in.cap) {
		// More captures than the scratch buffer holds: grow it and ask again.
		if err := in.grow(uint32(n * 12)); err != nil {
			return nil, err
		}
		n, err = in.call(in.fn.spans, uint64(lo), uint64(hi), uint64(in.out), uint64(in.cap/4/3))
		if err != nil {
			return nil, err
		}
	}
	b, ok := in.mod.Memory().Read(in.out, uint32(n*12))
	if !ok {
		return nil, errors.New("treesitter: read outside module memory")
	}
	for i := 0; i < int(n)*3; i++ {
		dst = append(dst, binary.LittleEndian.Uint32(b[4*i:]))
	}
	return dst, nil
}

// grow replaces the scratch buffer with one of at least size bytes.
func (in *Instance) grow(size uint32) error {
	if err := in.release(in.out); err != nil {
		return err
	}
	p, err := in.call(in.fn.alloc, uint64(size))
	if err != nil {
		return err
	}
	if p == 0 {
		return errors.New("treesitter: module is out of memory")
	}
	in.out, in.cap = uint32(p), size
	return nil
}

// TreeHash returns a hash of the current tree. With fresh set it hashes a new
// parse of the same text, so a caller can check that incremental parsing gives
// the same tree as parsing from scratch.
func (in *Instance) TreeHash(fresh bool) (uint64, error) {
	f := uint64(0)
	if fresh {
		f = 1
	}
	return in.call(in.fn.treeHash, f)
}

// TreeHasError reports whether the current tree contains an error node.
func (in *Instance) TreeHasError() (bool, error) {
	r, err := in.call(in.fn.treeHasError)
	return r != 0, err
}

// MemorySize returns the size of the instance's linear memory in bytes.
func (in *Instance) MemorySize() uint32 {
	return in.mod.Memory().Size()
}

// Close releases the instance and its memory.
func (in *Instance) Close() error {
	mu.Lock()
	defer mu.Unlock()
	return in.mod.Close(context.Background())
}
