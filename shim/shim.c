// shim.c is the interface between a language module and the editor. Each
// module holds one document (its text, parser and tree) and one highlight
// query. The grammar is chosen at build time with TS_LANG_FN, the name of the
// grammar's language function, so the same source builds every language.
//
// The host keeps the document's text up to date by applying each edit with
// doc_edit, then calls doc_parse to reparse incrementally once for all the
// edits it has applied, and doc_spans to read the highlight captures for a byte
// range. Positions are bytes. Rows and columns are computed here from the text,
// so the host passes only byte offsets.
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <tree_sitter/api.h>

#ifndef TS_LANG_FN
#define TS_LANG_FN tree_sitter_plain
#endif
const TSLanguage *TS_LANG_FN(void);

static TSParser *parser;
static TSTree *tree;
static TSQuery *query;
static char *buf;
static uint32_t len, cap;
static uint32_t query_err_off;

__attribute__((export_name("buf_alloc"))) void *buf_alloc(uint32_t n) { return malloc(n); }
__attribute__((export_name("buf_free"))) void buf_free(void *p) { free(p); }

// doc_init copies the document text and selects the grammar. It returns 0, or
// -1 if the grammar is not compatible with the runtime. The tree is discarded,
// so the next doc_parse must be a full parse.
__attribute__((export_name("doc_init"))) int doc_init(const char *src, uint32_t n) {
	if (!parser) parser = ts_parser_new();
	if (!ts_parser_set_language(parser, TS_LANG_FN())) return -1;
	if (tree) {
		ts_tree_delete(tree);
		tree = NULL;
	}
	free(buf);
	cap = n + n / 4 + 4096;
	buf = malloc(cap);
	if (n) memcpy(buf, src, n);
	len = n;
	return 0;
}

// doc_set_query compiles the highlight query for the grammar. It returns 0, or
// the TSQueryError code, and doc_query_error_offset then gives the byte offset
// of the error in the query text.
__attribute__((export_name("doc_set_query"))) int doc_set_query(const char *q, uint32_t n) {
	TSQueryError err = TSQueryErrorNone;
	if (query) {
		ts_query_delete(query);
		query = NULL;
	}
	query = ts_query_new(ts_parser_language(parser), q, n, &query_err_off, &err);
	return query ? 0 : (int)err;
}

__attribute__((export_name("doc_query_error_offset"))) uint32_t doc_query_error_offset(void) { return query_err_off; }

// doc_capture_count returns how many capture names the query defines.
__attribute__((export_name("doc_capture_count"))) uint32_t doc_capture_count(void) {
	return query ? ts_query_capture_count(query) : 0;
}

// doc_capture_name copies the name of capture i into out, at most max bytes,
// and returns the full length of the name.
__attribute__((export_name("doc_capture_name"))) uint32_t doc_capture_name(uint32_t i, char *out, uint32_t max) {
	uint32_t n = 0;
	const char *name = ts_query_capture_name_for_id(query, i, &n);
	memcpy(out, name, n < max ? n : max);
	return n;
}

// point_at returns the row and column of byte offset at in the current text.
static TSPoint point_at(uint32_t at) {
	TSPoint p = {0, 0};
	uint32_t line = 0;
	for (uint32_t i = 0; i < at; i++) {
		if (buf[i] == '\n') {
			p.row++;
			line = i + 1;
		}
	}
	p.column = at - line;
	return p;
}

// doc_edit replaces the bytes from start to old_end with the n bytes at text,
// and edits the tree to match. The text is not reparsed until doc_parse. It
// returns 0, or -1 if the range is not inside the document.
__attribute__((export_name("doc_edit"))) int doc_edit(uint32_t start, uint32_t old_end, const char *text, uint32_t n) {
	if (start > old_end || old_end > len) return -1;
	TSInputEdit e;
	e.start_byte = start;
	e.old_end_byte = old_end;
	e.new_end_byte = start + n;
	e.start_point = point_at(start);
	e.old_end_point = point_at(old_end);
	// The new end follows the inserted text. Its column counts from the last
	// newline in the text, or from the start column when there is none.
	uint32_t newlines = 0, after = 0;
	for (uint32_t i = 0; i < n; i++) {
		if (text[i] == '\n') {
			newlines++;
			after = i + 1;
		}
	}
	e.new_end_point.row = e.start_point.row + newlines;
	e.new_end_point.column = newlines ? n - after : e.start_point.column + n;

	uint32_t new_len = len - (old_end - start) + n;
	if (new_len + 1 > cap) {
		cap = new_len * 2 + 4096;
		buf = realloc(buf, cap);
	}
	memmove(buf + start + n, buf + old_end, len - old_end);
	if (n) memcpy(buf + start, text, n);
	len = new_len;
	if (tree) ts_tree_edit(tree, &e);
	return 0;
}

// doc_parse parses the document. With full set it parses from scratch,
// otherwise it reuses the edited tree. It returns 0, or -1 if the parser gave
// no tree.
__attribute__((export_name("doc_parse"))) int doc_parse(int full) {
	TSTree *next = ts_parser_parse_string(parser, full ? NULL : tree, buf, len);
	if (!next) return -1;
	if (tree) ts_tree_delete(tree);
	tree = next;
	return 0;
}

// regex_match is provided by the host, which evaluates #match? with Go's regexp
// package. It returns 1 when the expression pat (plen bytes) matches anywhere in
// text (tlen bytes). Go's syntax covers the expressions in the highlight queries,
// and matches the Rust regex crate that tree-sitter's own bindings use.
__attribute__((import_module("jamb"), import_name("regex_match"))) int32_t regex_match(const char *pat, uint32_t plen, const char *text, uint32_t tlen);

static int streq(const char *s, uint32_t n, const char *lit) { return strlen(lit) == n && memcmp(s, lit, n) == 0; }

// arg_text returns the text of a predicate argument: the text of the match's
// first node for a capture, or the string literal.
static void arg_text(const TSQueryMatch *m, const TSQueryPredicateStep *a, const char **text, uint32_t *n) {
	if (a->type == TSQueryPredicateStepTypeString) {
		*text = ts_query_string_value_for_id(query, a->value_id, n);
		return;
	}
	*text = "";
	*n = 0;
	for (uint16_t k = 0; k < m->capture_count; k++) {
		if (m->captures[k].index == a->value_id) {
			uint32_t s = ts_node_start_byte(m->captures[k].node);
			*text = buf + s;
			*n = ts_node_end_byte(m->captures[k].node) - s;
			return;
		}
	}
}

// check_predicate evaluates one text predicate of a match. The predicates are
// #eq?, #any-of?, #match? and their not- forms. Any other predicate, and any
// directive such as #is-not?, passes, as it does in the editors that use these
// queries.
static int check_predicate(const TSQueryMatch *m, const char *name, uint32_t nlen, const TSQueryPredicateStep *a, uint32_t n) {
	if (n < 2 || a[0].type != TSQueryPredicateStepTypeCapture) return 1;
	int neg = nlen > 4 && memcmp(name, "not-", 4) == 0;
	const char *base = neg ? name + 4 : name;
	uint32_t blen = neg ? nlen - 4 : nlen;
	const char *lhs;
	uint32_t ll;
	arg_text(m, &a[0], &lhs, &ll);
	int hit = 0;
	if (streq(base, blen, "eq?")) {
		const char *rhs;
		uint32_t rl;
		arg_text(m, &a[1], &rhs, &rl);
		hit = ll == rl && memcmp(lhs, rhs, ll) == 0;
	} else if (streq(base, blen, "any-of?")) {
		for (uint32_t k = 1; k < n; k++) {
			const char *rhs;
			uint32_t rl;
			arg_text(m, &a[k], &rhs, &rl);
			if (ll == rl && memcmp(lhs, rhs, ll) == 0) hit = 1;
		}
	} else if (streq(base, blen, "match?")) {
		const char *pat;
		uint32_t pl;
		arg_text(m, &a[1], &pat, &pl);
		hit = regex_match(pat, pl, lhs, ll) != 0;
	} else {
		return 1;
	}
	return neg ? !hit : hit;
}

// predicate_passes reports whether the match satisfies every text predicate of
// its pattern. Each predicate is a name string, its arguments, and a done step.
static int predicate_passes(const TSQueryMatch *m) {
	uint32_t n = 0;
	const TSQueryPredicateStep *st = ts_query_predicates_for_pattern(query, m->pattern_index, &n);
	for (uint32_t i = 0; i < n;) {
		uint32_t j = i + 1;
		while (j < n && st[j].type != TSQueryPredicateStepTypeDone) j++;
		if (st[i].type == TSQueryPredicateStepTypeString) {
			uint32_t nlen;
			const char *name = ts_query_string_value_for_id(query, st[i].value_id, &nlen);
			if (!check_predicate(m, name, nlen, st + i + 1, j - i - 1)) return 0;
		}
		i = j + 1;
	}
	return 1;
}

// doc_spans writes the captures of the highlight query that intersect bytes lo
// to hi, as three uint32 each (start byte, end byte, capture index), in the
// order the query yields them. Captures of matches that fail a text predicate
// are left out. It writes at most max captures and returns the total number, so
// the host can retry with a larger buffer.
__attribute__((export_name("doc_spans"))) uint32_t doc_spans(uint32_t lo, uint32_t hi, uint32_t *out, uint32_t max) {
	if (!tree || !query) return 0;
	TSQueryCursor *qc = ts_query_cursor_new();
	ts_query_cursor_set_byte_range(qc, lo, hi);
	ts_query_cursor_exec(qc, query, ts_tree_root_node(tree));
	TSQueryMatch m;
	uint32_t idx, n = 0;
	while (ts_query_cursor_next_capture(qc, &m, &idx)) {
		if (!predicate_passes(&m)) continue;
		if (n < max) {
			TSQueryCapture c = m.captures[idx];
			out[3 * n] = ts_node_start_byte(c.node);
			out[3 * n + 1] = ts_node_end_byte(c.node);
			out[3 * n + 2] = c.index;
		}
		n++;
	}
	ts_query_cursor_delete(qc);
	return n;
}

// doc_tree_has_error reports whether the current tree contains an error node.
__attribute__((export_name("doc_tree_has_error"))) int doc_tree_has_error(void) {
	return tree ? ts_node_has_error(ts_tree_root_node(tree)) : 0;
}

// doc_tree_hash hashes the S-expression of the current tree. With fresh set it
// hashes a from-scratch parse of the same text instead, so a test can check
// that incremental parsing gives the same tree.
__attribute__((export_name("doc_tree_hash"))) uint64_t doc_tree_hash(int fresh) {
	TSTree *t = fresh ? ts_parser_parse_string(parser, NULL, buf, len) : tree;
	if (!t) return 0;
	char *s = ts_node_string(ts_tree_root_node(t));
	uint64_t h = 1469598103934665603ULL;
	for (char *p = s; *p; p++) h = (h ^ (uint8_t)*p) * 1099511628211ULL;
	free(s);
	if (fresh) ts_tree_delete(t);
	return h;
}
