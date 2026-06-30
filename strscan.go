// Package strscan is a pure-Go, CGO-free reimplementation of Ruby's
// StringScanner (the stdlib "strscan" library) matching MRI 4.0.5's observable
// behavior. A Scanner walks a string left to right, matching regular
// expressions anchored at — or searched forward from — the current scan
// position and advancing past each match, recording the last match so the
// caller can read it back (the whole match, capture groups, pre/post text).
//
// Patterns are matched by the sibling pure-Go Onigmo engine
// (github.com/go-ruby-regexp/regexp), so the scanner's regex semantics — UTF-8
// awareness, named groups, character classes — match Ruby exactly.
//
// Positions are byte offsets into the underlying string, exactly as MRI's
// StringScanner#pos reports them; Scanner.CharPos returns the character index
// (the number of UTF-8 code points before the position) the way MRI's #charpos
// does. For all-ASCII input the two coincide.
//
// A Scanner is a mutable cursor and is NOT safe for concurrent use.
package strscan

import (
	"fmt"
	"sync"
	"unicode/utf8"

	onigmo "github.com/go-ruby-regexp/regexp"
)

// Error is returned by operations that Ruby's StringScanner reports through a
// StringScanner::Error, currently only Unscan with nothing to undo.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

// Scanner is a position cursor over a string driven by regular-expression
// matches, mirroring Ruby's StringScanner.
type Scanner struct {
	str string // the string being scanned
	pos int    // current byte offset into str (the scan position)

	// last-match state, all reset to the no-match sentinel by a failed match
	// (matchValid=false). When matchValid is true a match was recorded.
	matchValid bool
	md         *onigmo.MatchData // the recorded match (nil for getch)
	matched    string            // the matched text (md[0], or the char for getch)
	matchBeg   int               // byte offset of the match start in str
	matchEnd   int               // byte offset just past the match in str

	// prevPos records the scan position before the last advancing operation so
	// that Unscan can restore it; unscannable is true when there is nothing to
	// undo (no prior advancing match), making Unscan an error.
	prevPos     int
	unscannable bool
}

// New returns a Scanner positioned at the start of s with no recorded match.
func New(s string) *Scanner {
	return &Scanner{str: s, unscannable: true}
}

// regexpCache memoizes compiled patterns so repeated scans with the same source
// pattern do not recompile. Patterns come from the caller (rbgo passes the
// Regexp#source), so the set is small and bounded in practice, and an unbounded
// map is fine: the live key set is the handful of distinct regexp literals a
// lexer reuses. The cache is process-wide and shared across every Scanner so the
// win carries across scanners; it is a sync.Map, making the read-mostly hot path
// lock-free and safe under concurrent Scanners (the cache passes -race).
//
// Only the compiled value is keyed by the source string. The flags a caller
// wants are embedded inline in that source (e.g. "(?imx)..."), so two callers
// passing the same source get the same compiled regexp — caching by source is
// correctness-safe and byte-for-byte equivalent to compiling every time.
var regexpCache sync.Map // map[string]*onigmo.Regexp

// compile returns the compiled form of pattern, caching it. A malformed pattern
// returns an error, which the scanning methods surface as "no match" — Ruby
// would have raised at Regexp construction, before the scan, so a bad pattern
// never matches here. Compile errors are NOT cached: error behavior is identical
// to compiling every call, and the malformed-pattern set is not a hot path.
func compile(pattern string) (*onigmo.Regexp, error) {
	if v, ok := regexpCache.Load(pattern); ok {
		return v.(*onigmo.Regexp), nil
	}
	re, err := onigmo.Compile(pattern)
	if err != nil {
		return nil, err
	}
	// LoadOrStore keeps a single shared *Regexp per source even when two
	// goroutines miss and compile concurrently: the first store wins, losers
	// drop their duplicate and reuse the winner.
	actual, _ := regexpCache.LoadOrStore(pattern, re)
	return actual.(*onigmo.Regexp), nil
}

// matchAt matches pattern against the remainder str[pos:]; the returned offsets
// are translated back into absolute offsets in str. anchored requires the match
// to begin exactly at pos (Ruby's \G / scan semantics); otherwise the first
// match anywhere in the remainder is returned (scan_until semantics). ok is
// false when the pattern is malformed or does not match.
func (s *Scanner) matchAt(pattern string, anchored bool) (md *onigmo.MatchData, beg, end int, ok bool) {
	re, err := compile(pattern)
	if err != nil {
		return nil, 0, 0, false
	}
	rest := s.str[s.pos:]
	m := re.Match(rest)
	if m == nil {
		return nil, 0, 0, false
	}
	if anchored && m.Begin(0) != 0 {
		return nil, 0, 0, false
	}
	return m, s.pos + m.Begin(0), s.pos + m.End(0), true
}

// record stores a successful match and remembers the pre-advance position so
// Unscan can undo it; advancing to newPos.
func (s *Scanner) record(md *onigmo.MatchData, beg, end, newPos int) {
	s.prevPos = s.pos
	s.matchValid = true
	s.md = md
	s.matched = s.str[beg:end]
	s.matchBeg = beg
	s.matchEnd = end
	s.pos = newPos
	s.unscannable = false
}

// clearMatch drops any recorded match (a failed scan), matching MRI which nils
// out the last match on a miss. It also makes Unscan an error.
func (s *Scanner) clearMatch() {
	s.matchValid = false
	s.md = nil
	s.matched = ""
	s.unscannable = true
}

// Scan matches pattern anchored at the current position; on success it records
// the match, advances past it, and returns the matched text. On failure it
// returns "" / false, clears the recorded match, and leaves the position.
// (Ruby's StringScanner#scan.)
func (s *Scanner) Scan(pattern string) (string, bool) {
	md, beg, end, ok := s.matchAt(pattern, true)
	if !ok {
		s.clearMatch()
		return "", false
	}
	s.record(md, beg, end, end)
	return s.matched, true
}

// ScanUntil advances to and past the next occurrence of pattern anywhere ahead,
// returning everything from the old position through the match. On no match it
// returns "" / false and does not move. #matched holds just the pattern match.
// (Ruby's StringScanner#scan_until.)
func (s *Scanner) ScanUntil(pattern string) (string, bool) {
	md, beg, end, ok := s.matchAt(pattern, false)
	if !ok {
		s.clearMatch()
		return "", false
	}
	result := s.str[s.pos:end]
	s.record(md, beg, end, end)
	return result, true
}

// Skip behaves like Scan but returns the byte length of the matched text rather
// than the text; n is -1 / false on no match. (Ruby's StringScanner#skip.)
func (s *Scanner) Skip(pattern string) (int, bool) {
	if str, ok := s.Scan(pattern); ok {
		return len(str), true
	}
	return -1, false
}

// SkipUntil behaves like ScanUntil but returns the byte length from the old
// position through the match (-1 / false on no match). (Ruby's #skip_until.)
func (s *Scanner) SkipUntil(pattern string) (int, bool) {
	if str, ok := s.ScanUntil(pattern); ok {
		return len(str), true
	}
	return -1, false
}

// Match reports the byte length of a match anchored at the current position
// WITHOUT advancing it, recording the match; length is -1 / false on no match.
// (Ruby's StringScanner#match?.)
func (s *Scanner) Match(pattern string) (int, bool) {
	md, beg, end, ok := s.matchAt(pattern, true)
	if !ok {
		s.clearMatch()
		return -1, false
	}
	s.record(md, beg, end, s.pos) // record but do not advance
	return end - beg, true
}

// Check matches pattern anchored at the current position WITHOUT advancing,
// returning the matched text. "" / false and a cleared match on no match.
// (Ruby's StringScanner#check.)
func (s *Scanner) Check(pattern string) (string, bool) {
	md, beg, end, ok := s.matchAt(pattern, true)
	if !ok {
		s.clearMatch()
		return "", false
	}
	s.record(md, beg, end, s.pos)
	return s.matched, true
}

// CheckUntil searches forward for pattern like ScanUntil but does NOT advance,
// returning everything from the current position through the match. "" / false
// on no match. (Ruby's StringScanner#check_until.)
func (s *Scanner) CheckUntil(pattern string) (string, bool) {
	md, beg, end, ok := s.matchAt(pattern, false)
	if !ok {
		s.clearMatch()
		return "", false
	}
	result := s.str[s.pos:end]
	s.record(md, beg, end, s.pos)
	return result, true
}

// Peek returns up to len bytes... up to n characters? Ruby's #peek takes a byte
// length and returns that many bytes from the current position (clamped to the
// end), without advancing. (Ruby's StringScanner#peek.)
func (s *Scanner) Peek(n int) string {
	if n <= 0 {
		return ""
	}
	end := s.pos + n
	if end > len(s.str) {
		end = len(s.str)
	}
	return s.str[s.pos:end]
}

// Getch returns the single character at the current position and advances one
// character; "" / false at end-of-string. It records the character as the
// matched text (with no capture groups). (Ruby's StringScanner#getch.)
func (s *Scanner) Getch() (string, bool) {
	if s.EOS() {
		s.clearMatch()
		return "", false
	}
	_, sz := utf8.DecodeRuneInString(s.str[s.pos:])
	beg, end := s.pos, s.pos+sz
	s.prevPos = s.pos
	s.matchValid = true
	s.md = nil // getch has no MatchData; [0] is the char, every group is nil
	s.matched = s.str[beg:end]
	s.matchBeg = beg
	s.matchEnd = end
	s.pos = end
	s.unscannable = false
	return s.matched, true
}

// Pos returns the current scan position as a byte offset. (Ruby's #pos.)
func (s *Scanner) Pos() int { return s.pos }

// CharPos returns the current scan position as a character index — the number
// of UTF-8 code points in the text before the position. (Ruby's #charpos.)
func (s *Scanner) CharPos() int {
	return utf8.RuneCountInString(s.str[:s.pos])
}

// SetPos moves the scan position to the byte offset n. A negative n counts back
// from the end. An offset outside [0, len] returns an error and leaves the
// position unchanged, mirroring MRI's RangeError. Setting the position clears
// nothing about the recorded match but, like MRI, makes the next Unscan invalid.
func (s *Scanner) SetPos(n int) error {
	if n < 0 {
		n += len(s.str)
	}
	if n < 0 || n > len(s.str) {
		return &Error{msg: fmt.Sprintf("strscan: index out of range: %d", n)}
	}
	s.pos = n
	s.unscannable = true
	return nil
}

// Rest is the unscanned remainder of the string. (Ruby's #rest.)
func (s *Scanner) Rest() string { return s.str[s.pos:] }

// RestSize is the byte length of the unscanned remainder. (Ruby's #rest_size.)
func (s *Scanner) RestSize() int { return len(s.str) - s.pos }

// EOS reports whether the position is at (or past) the end of the string.
// (Ruby's #eos?.)
func (s *Scanner) EOS() bool { return s.pos >= len(s.str) }

// Beginning reports whether the position is at the beginning of a line: the
// start of the string, or immediately after a newline. (Ruby's #bol? /
// #beginning_of_line?.) It is false at a position past the end of the string.
func (s *Scanner) Beginning() bool {
	if s.pos > len(s.str) {
		return false
	}
	if s.pos == 0 {
		return true
	}
	return s.str[s.pos-1] == '\n'
}

// Terminate jumps the position to the end of the string and clears the recorded
// match; it returns the receiver for chaining. (Ruby's #terminate.)
func (s *Scanner) Terminate() *Scanner {
	s.pos = len(s.str)
	s.clearMatch()
	return s
}

// Reset returns the position to the start and clears the recorded match,
// returning the receiver. (Ruby's #reset.)
func (s *Scanner) Reset() *Scanner {
	s.pos = 0
	s.clearMatch()
	return s
}

// Matched returns the text of the most recent match, or "" / false when the
// last match attempt failed or nothing has matched yet. (Ruby's #matched /
// #matched?.)
func (s *Scanner) Matched() (string, bool) {
	if !s.matchValid {
		return "", false
	}
	return s.matched, true
}

// MatchedSize returns the byte length of the most recent match, or -1 when the
// last match attempt failed or nothing has matched yet. (Ruby's #matched_size.)
func (s *Scanner) MatchedSize() int {
	if !s.matchValid {
		return -1
	}
	return s.matchEnd - s.matchBeg
}

// PreMatch returns the text before the start of the most recent match, or
// "" / false when there is no current match. (Ruby's #pre_match.)
func (s *Scanner) PreMatch() (string, bool) {
	if !s.matchValid {
		return "", false
	}
	return s.str[:s.matchBeg], true
}

// PostMatch returns the text after the end of the most recent match, or
// "" / false when there is no current match. (Ruby's #post_match.)
func (s *Scanner) PostMatch() (string, bool) {
	if !s.matchValid {
		return "", false
	}
	return s.str[s.matchEnd:], true
}

// Group returns the i-th capture group of the most recent match: 0 is the whole
// match, a positive index the n-th group. It returns "" / false when there is
// no current match, the index is out of range, or the group did not
// participate. For a getch match only group 0 (the character) is present.
// (Ruby's StringScanner#[] with an Integer.)
func (s *Scanner) Group(i int) (string, bool) {
	if !s.matchValid {
		return "", false
	}
	if s.md == nil { // getch: only [0] is the char, every group is absent
		if i == 0 {
			return s.matched, true
		}
		return "", false
	}
	if i < 0 || i > s.md.NGroups() {
		return "", false
	}
	if s.md.Begin(i) < 0 {
		return "", false // group did not participate
	}
	return s.md.Str(i), true
}

// GroupName returns the capture group with the given name from the most recent
// match, or "" / false when there is no current match, no group has that name,
// or the group did not participate. (Ruby's StringScanner#[] with a
// Symbol/String.)
func (s *Scanner) GroupName(name string) (string, bool) {
	if !s.matchValid || s.md == nil {
		return "", false
	}
	idx := s.md.IndexOfName(name)
	if idx < 0 {
		return "", false
	}
	if s.md.Begin(idx) < 0 {
		return "", false
	}
	return s.md.Str(idx), true
}

// Unscan undoes the most recent advancing match, restoring the position to
// where it was before that match and clearing the recorded match. It returns an
// error (Ruby's StringScanner::Error) when there is nothing to undo — no prior
// successful scan/skip/getch, or the last match attempt failed. (Ruby's
// #unscan.)
func (s *Scanner) Unscan() error {
	if s.unscannable {
		return &Error{msg: "strscan: unscan failed: previous match record not exist"}
	}
	s.pos = s.prevPos
	s.matchValid = false
	s.md = nil
	s.matched = ""
	s.unscannable = true
	return nil
}

// Concat appends more text to the end of the scanned string without moving the
// position, so the scanner can keep going past what was previously the end.
// It returns the receiver. (Ruby's StringScanner#<< / #concat.)
func (s *Scanner) Concat(more string) *Scanner {
	s.str += more
	return s
}

// String returns the whole string being scanned. (Ruby's #string.)
func (s *Scanner) String() string { return s.str }

// SetString replaces the string being scanned, resets the position to the
// start, and clears the recorded match. (Ruby's #string=.)
func (s *Scanner) SetString(str string) {
	s.str = str
	s.pos = 0
	s.clearMatch()
}
