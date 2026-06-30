package strscan

import (
	"strings"
	"testing"

	onigmo "github.com/go-ruby-regexp/regexp"
)

// benchInput and benchPatterns model a lexer: a small, fixed set of regexp
// literals applied over and over to tokenize a representative input. This is the
// exact shape that exposed the per-call recompile cost (rbgo measured strscan
// ~71x slower than MRI from recompiling the same handful of patterns).
var (
	benchInput    = strings.Repeat("foo123 + bar456 - baz789 * qux000 / quux ; ", 64)
	benchPatterns = []string{
		`[A-Za-z_][A-Za-z0-9_]*`, // identifier
		`[0-9]+`,                 // number
		`\s+`,                    // whitespace
		`[-+*/;]`,                // operator/punct
	}
)

// lexLoopCached runs a full tokenizing pass over benchInput using the production
// (cached) compile path: each distinct pattern compiles at most once for the
// whole process and every subsequent Scan reuses the cached *Regexp.
func lexLoopCached() int {
	s := New(benchInput)
	n := 0
	for !s.EOS() {
		matched := false
		for _, p := range benchPatterns {
			if _, ok := s.Scan(p); ok {
				n++
				matched = true
				break
			}
		}
		if !matched {
			s.Getch() // never happens for benchInput, but keeps the loop total
		}
	}
	return n
}

// lexLoopNoCache is an apples-to-apples copy of lexLoopCached that bypasses the
// cache and calls onigmo.Compile on every Scan, reproducing the pre-fix
// behavior. It exists only to quantify what the cache saves.
func lexLoopNoCache() int {
	s := New(benchInput)
	n := 0
	for !s.EOS() {
		matched := false
		for _, p := range benchPatterns {
			re, err := onigmo.Compile(p) // recompile every call (the bug)
			if err != nil {
				continue
			}
			rest := s.str[s.pos:]
			m := re.Match(rest)
			if m == nil || m.Begin(0) != 0 {
				continue
			}
			s.pos += m.End(0)
			n++
			matched = true
			break
		}
		if !matched {
			s.Getch()
		}
	}
	return n
}

// BenchmarkScanLoopCached measures the production path (compile cache on).
func BenchmarkScanLoopCached(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = lexLoopCached()
	}
}

// BenchmarkScanLoopNoCache measures the pre-fix path (recompile every Scan),
// for direct comparison with BenchmarkScanLoopCached.
func BenchmarkScanLoopNoCache(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = lexLoopNoCache()
	}
}
