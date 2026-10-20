package treesitter

import (
	"context"
	"regexp"
	"sync"

	"github.com/tetratelabs/wazero/api"
)

// compiledRegexes caches the expressions of #match? predicates by their source
// text. A nil entry records an expression that does not compile, which then
// never matches.
var compiledRegexes sync.Map

// regexMatch is the host function behind the shim's regex_match import. It reads
// the expression and the text from the calling module's memory and reports
// whether the expression matches anywhere in the text.
func regexMatch(_ context.Context, m api.Module, pat, plen, text, tlen uint32) uint32 {
	mem := m.Memory()
	p, ok := mem.Read(pat, plen)
	if !ok {
		return 0
	}
	t, ok := mem.Read(text, tlen)
	if !ok {
		return 0
	}
	re := compiledRegex(string(p))
	if re != nil && re.Match(t) {
		return 1
	}
	return 0
}

// compiledRegex returns the compiled form of src, or nil when src does not
// compile.
func compiledRegex(src string) *regexp.Regexp {
	if v, ok := compiledRegexes.Load(src); ok {
		return v.(*regexp.Regexp)
	}
	re, err := regexp.Compile(src)
	if err != nil {
		re = nil
	}
	compiledRegexes.Store(src, re)
	return re
}
