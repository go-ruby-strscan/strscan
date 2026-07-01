package strscan

import (
	"strconv"
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

// lexLoopSize tokenizes a repeat-scaled copy of the base lexer input using the
// production anchored Scan path. It exists to prove that the per-token anchored
// scan is O(match length): doubling the input should roughly double the total
// work, so ns/op divided by the repeat factor stays flat. Before the MatchAt
// fix each anchored Scan was O(remaining length) — MatchAt scanned the whole
// tail and rejected an off-position match — making a full pass O(n²), so the
// per-token cost grew with input size.
func lexLoopSize(repeat int) int {
	input := strings.Repeat("foo123 + bar456 - baz789 * qux000 / quux ; ", repeat)
	s := New(input)
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
			s.Getch()
		}
	}
	return n
}

// BenchmarkScanScaling runs the anchored tokenize loop at ×1/×2/×4 input so a
// benchmark run can confirm linear (not quadratic) scaling: the per-repeat cost
// (ns/op ÷ size) should stay roughly constant across sizes.
func BenchmarkScanScaling(b *testing.B) {
	for _, size := range []int{64, 128, 256} {
		size := size
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = lexLoopSize(size)
			}
		})
	}
}
