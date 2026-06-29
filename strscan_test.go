package strscan

import (
	"testing"
)

// These deterministic, ruby-free tests exercise every method and every branch
// so the suite reaches 100% coverage on the qemu / no-ruby CI lanes where the
// MRI oracle self-skips.

func TestScanAdvanceAndMiss(t *testing.T) {
	s := New("hello world")
	if got, ok := s.Scan(`hello`); !ok || got != "hello" {
		t.Fatalf("Scan hello = %q,%v", got, ok)
	}
	if s.Pos() != 5 {
		t.Fatalf("pos = %d, want 5", s.Pos())
	}
	// A non-anchored-at-pos pattern must NOT match even though it exists ahead.
	if _, ok := s.Scan(`world`); ok {
		t.Fatal("Scan world should miss (space at pos)")
	}
	// Miss clears the recorded match.
	if _, ok := s.Matched(); ok {
		t.Fatal("Matched should be cleared after a failed Scan")
	}
	if got, ok := s.Scan(` `); !ok || got != " " {
		t.Fatalf("Scan space = %q,%v", got, ok)
	}
	if got, ok := s.Scan(`world`); !ok || got != "world" {
		t.Fatalf("Scan world = %q,%v", got, ok)
	}
	if !s.EOS() {
		t.Fatal("expected EOS")
	}
}

func TestScanUntil(t *testing.T) {
	s := New("foo=bar")
	got, ok := s.ScanUntil(`=`)
	if !ok || got != "foo=" {
		t.Fatalf("ScanUntil = %q,%v", got, ok)
	}
	if m, _ := s.Matched(); m != "=" {
		t.Fatalf("matched = %q, want =", m)
	}
	if pre, _ := s.PreMatch(); pre != "foo" {
		t.Fatalf("pre = %q", pre)
	}
	if post, _ := s.PostMatch(); post != "bar" {
		t.Fatalf("post = %q", post)
	}
	// Miss leaves position and clears the match.
	if _, ok := s.ScanUntil(`zzz`); ok {
		t.Fatal("ScanUntil zzz should miss")
	}
	if _, ok := s.Matched(); ok {
		t.Fatal("match should be cleared after failed ScanUntil")
	}
}

func TestSkipAndSkipUntil(t *testing.T) {
	s := New("aaabbb")
	if n, ok := s.Skip(`a+`); !ok || n != 3 {
		t.Fatalf("Skip = %d,%v", n, ok)
	}
	if n, ok := s.Skip(`zzz`); ok || n != -1 {
		t.Fatalf("Skip miss = %d,%v", n, ok)
	}
	s2 := New("xx--yy")
	if n, ok := s2.SkipUntil(`--`); !ok || n != 4 {
		t.Fatalf("SkipUntil = %d,%v", n, ok)
	}
	if n, ok := s2.SkipUntil(`zzz`); ok || n != -1 {
		t.Fatalf("SkipUntil miss = %d,%v", n, ok)
	}
}

func TestMatchNoAdvance(t *testing.T) {
	s := New("number 42")
	if n, ok := s.Match(`\w+`); !ok || n != 6 {
		t.Fatalf("Match = %d,%v", n, ok)
	}
	if s.Pos() != 0 {
		t.Fatalf("Match must not advance, pos = %d", s.Pos())
	}
	if n, ok := s.Match(`\d+`); ok || n != -1 {
		t.Fatalf("Match digits should miss at pos 0, got %d,%v", n, ok)
	}
}

func TestCheckAndCheckUntil(t *testing.T) {
	s := New("abcdef")
	if got, ok := s.Check(`abc`); !ok || got != "abc" {
		t.Fatalf("Check = %q,%v", got, ok)
	}
	if s.Pos() != 0 {
		t.Fatalf("Check must not advance, pos = %d", s.Pos())
	}
	if _, ok := s.Check(`xyz`); ok {
		t.Fatal("Check xyz should miss")
	}
	got, ok := s.CheckUntil(`de`)
	if !ok || got != "abcde" {
		t.Fatalf("CheckUntil = %q,%v", got, ok)
	}
	if s.Pos() != 0 {
		t.Fatalf("CheckUntil must not advance, pos = %d", s.Pos())
	}
	if _, ok := s.CheckUntil(`zzz`); ok {
		t.Fatal("CheckUntil zzz should miss")
	}
}

func TestPeekAndGetch(t *testing.T) {
	s := New("héllo")
	if p := s.Peek(3); p != "hé" { // 'h'(1) + 'é'(2) = 3 bytes
		t.Fatalf("Peek(3) = %q", p)
	}
	if p := s.Peek(0); p != "" {
		t.Fatalf("Peek(0) = %q", p)
	}
	if p := s.Peek(-1); p != "" {
		t.Fatalf("Peek(-1) = %q", p)
	}
	if p := s.Peek(100); p != "héllo" {
		t.Fatalf("Peek(100) = %q", p)
	}
	if c, ok := s.Getch(); !ok || c != "h" {
		t.Fatalf("Getch = %q,%v", c, ok)
	}
	if c, ok := s.Getch(); !ok || c != "é" {
		t.Fatalf("Getch = %q,%v", c, ok)
	}
	if s.Pos() != 3 || s.CharPos() != 2 {
		t.Fatalf("pos=%d charpos=%d", s.Pos(), s.CharPos())
	}
	// getch records [0] but no groups.
	if g, ok := s.Group(0); !ok || g != "é" {
		t.Fatalf("Group(0) after getch = %q,%v", g, ok)
	}
	if _, ok := s.Group(1); ok {
		t.Fatal("Group(1) after getch should be absent")
	}
	if _, ok := s.GroupName("x"); ok {
		t.Fatal("GroupName after getch should be absent")
	}
}

func TestGetchAtEOS(t *testing.T) {
	s := New("")
	if c, ok := s.Getch(); ok || c != "" {
		t.Fatalf("Getch on empty = %q,%v", c, ok)
	}
	if _, ok := s.Matched(); ok {
		t.Fatal("match cleared on getch at EOS")
	}
}

func TestPosCharPosSetPos(t *testing.T) {
	s := New("héllo")
	if err := s.SetPos(3); err != nil {
		t.Fatalf("SetPos(3): %v", err)
	}
	if s.Rest() != "llo" || s.CharPos() != 2 {
		t.Fatalf("rest=%q charpos=%d", s.Rest(), s.CharPos())
	}
	// negative counts back from the end (byte length 6).
	if err := s.SetPos(-2); err != nil {
		t.Fatalf("SetPos(-2): %v", err)
	}
	if s.Pos() != 4 {
		t.Fatalf("pos after SetPos(-2) = %d, want 4", s.Pos())
	}
	// out of range high and low.
	if err := s.SetPos(99); err == nil {
		t.Fatal("SetPos(99) should error")
	}
	if err := s.SetPos(-99); err == nil {
		t.Fatal("SetPos(-99) should error")
	}
	// boundary == len is allowed.
	if err := s.SetPos(6); err != nil {
		t.Fatalf("SetPos(6): %v", err)
	}
	if !s.EOS() {
		t.Fatal("expected EOS at pos==len")
	}
}

func TestRestSizeEOSBeginning(t *testing.T) {
	s := New("a\nb")
	if s.RestSize() != 3 {
		t.Fatalf("RestSize = %d", s.RestSize())
	}
	if !s.Beginning() {
		t.Fatal("Beginning at start should be true")
	}
	s.Scan(`a`)
	if s.Beginning() {
		t.Fatal("Beginning after 'a' should be false")
	}
	s.Scan(`\n`)
	if !s.Beginning() {
		t.Fatal("Beginning after newline should be true")
	}
	// past the end is not a beginning of line.
	s.Terminate()
	_ = s.SetPos(3)
	if s.Beginning() { // pos==len, prev char is 'b' -> false
		t.Fatal("Beginning at end (after 'b') should be false")
	}
	// force pos beyond len via Concat shrink? Instead test the pos>len guard.
	sp := New("xy")
	sp.Concat("z") // len 3
	_ = sp.SetPos(3)
	sp.str = "x" // shrink underlying so pos>len, exercising the guard
	if sp.Beginning() {
		t.Fatal("Beginning with pos>len should be false")
	}
}

func TestTerminateReset(t *testing.T) {
	s := New("abc")
	s.Scan(`a`)
	if got := s.Terminate(); got != s {
		t.Fatal("Terminate should return receiver")
	}
	if !s.EOS() {
		t.Fatal("expected EOS after Terminate")
	}
	if _, ok := s.Matched(); ok {
		t.Fatal("match cleared after Terminate")
	}
	if got := s.Reset(); got != s {
		t.Fatal("Reset should return receiver")
	}
	if s.Pos() != 0 {
		t.Fatalf("pos after Reset = %d", s.Pos())
	}
}

func TestMatchedFamily(t *testing.T) {
	s := New("2026-06-29")
	if _, ok := s.Matched(); ok {
		t.Fatal("no match yet")
	}
	if s.MatchedSize() != -1 {
		t.Fatal("MatchedSize -1 before any match")
	}
	if _, ok := s.PreMatch(); ok {
		t.Fatal("PreMatch absent before match")
	}
	if _, ok := s.PostMatch(); ok {
		t.Fatal("PostMatch absent before match")
	}
	if _, ok := s.Group(0); ok {
		t.Fatal("Group absent before match")
	}
	if _, ok := s.GroupName("y"); ok {
		t.Fatal("GroupName absent before match")
	}
	s.Scan(`(?<y>\d+)-(?<m>\d+)-(?<d>\d+)`)
	if m, _ := s.Matched(); m != "2026-06-29" {
		t.Fatalf("matched = %q", m)
	}
	if s.MatchedSize() != 10 {
		t.Fatalf("matched_size = %d", s.MatchedSize())
	}
	if g, _ := s.Group(0); g != "2026-06-29" {
		t.Fatalf("Group(0) = %q", g)
	}
	if g, _ := s.Group(2); g != "06" {
		t.Fatalf("Group(2) = %q", g)
	}
	if g, _ := s.GroupName("d"); g != "29" {
		t.Fatalf("GroupName d = %q", g)
	}
	if _, ok := s.Group(-1); ok {
		t.Fatal("Group(-1) out of range")
	}
	if _, ok := s.Group(99); ok {
		t.Fatal("Group(99) out of range")
	}
	if _, ok := s.GroupName("nope"); ok {
		t.Fatal("GroupName nope absent")
	}
}

func TestNonParticipatingGroups(t *testing.T) {
	s := New("color")
	s.Scan(`colou?(r)(?<plural>s)?`)
	if g, ok := s.Group(1); !ok || g != "r" {
		t.Fatalf("Group(1) = %q,%v", g, ok)
	}
	// (s)? did not participate.
	if _, ok := s.Group(2); ok {
		t.Fatal("Group(2) should be non-participating")
	}
	if _, ok := s.GroupName("plural"); ok {
		t.Fatal("named non-participating group should be absent")
	}
}

func TestUnscan(t *testing.T) {
	s := New("abcdef")
	// nothing to undo yet.
	if err := s.Unscan(); err == nil {
		t.Fatal("Unscan with no prior match should error")
	}
	s.Scan(`ab`)
	s.Scan(`cd`)
	if s.Pos() != 4 {
		t.Fatalf("pos = %d", s.Pos())
	}
	if err := s.Unscan(); err != nil {
		t.Fatalf("Unscan: %v", err)
	}
	if s.Pos() != 2 {
		t.Fatalf("pos after unscan = %d, want 2", s.Pos())
	}
	// second consecutive unscan is an error.
	if err := s.Unscan(); err == nil {
		t.Fatal("double Unscan should error")
	}
	// unscan after a failed scan is an error (failed scan clears the record).
	s.Scan(`cd`)
	s.Scan(`zz`) // miss
	if err := s.Unscan(); err == nil {
		t.Fatal("Unscan after failed scan should error")
	}
	// unscan after getch and after skip.
	g := New("abc")
	g.Getch()
	if err := g.Unscan(); err != nil || g.Pos() != 0 {
		t.Fatalf("Unscan after getch: %v pos=%d", err, g.Pos())
	}
	sk := New("abc")
	sk.Skip(`ab`)
	if err := sk.Unscan(); err != nil || sk.Pos() != 0 {
		t.Fatalf("Unscan after skip: %v pos=%d", err, sk.Pos())
	}
}

func TestSetPosMakesUnscanInvalid(t *testing.T) {
	s := New("abc")
	s.Scan(`a`)
	_ = s.SetPos(0)
	if err := s.Unscan(); err == nil {
		t.Fatal("Unscan after SetPos should error")
	}
}

func TestConcatStringSetString(t *testing.T) {
	s := New("ab")
	if got, ok := s.Scan(`ab`); !ok || got == "" {
		t.Fatal("scan ab")
	}
	if s.Concat("cd") != s {
		t.Fatal("Concat returns receiver")
	}
	if s.String() != "abcd" {
		t.Fatalf("String = %q", s.String())
	}
	if r := s.Rest(); r != "cd" {
		t.Fatalf("Rest after Concat = %q", r)
	}
	if got, ok := s.Scan(`cd`); !ok || got != "cd" {
		t.Fatalf("Scan after Concat = %q,%v", got, ok)
	}
	s.SetString("xyz")
	if s.String() != "xyz" || s.Pos() != 0 {
		t.Fatalf("after SetString string=%q pos=%d", s.String(), s.Pos())
	}
	if _, ok := s.Matched(); ok {
		t.Fatal("SetString clears match")
	}
}

func TestEmptyAndWildcardMatch(t *testing.T) {
	s := New("abc")
	got, ok := s.Scan(`x*`) // empty match at pos 0
	if !ok || got != "" {
		t.Fatalf("Scan x* = %q,%v", got, ok)
	}
	if s.Pos() != 0 {
		t.Fatalf("empty match must not advance, pos=%d", s.Pos())
	}
	if s.MatchedSize() != 0 {
		t.Fatalf("matched_size of empty = %d", s.MatchedSize())
	}
}

func TestMalformedPatternMisses(t *testing.T) {
	// A malformed pattern would have raised in Ruby at Regexp construction; here
	// it surfaces as a clean miss across the matching methods rather than a panic.
	s := New("abc")
	if _, ok := s.Scan(`(`); ok {
		t.Fatal("malformed Scan should miss")
	}
	if _, ok := s.ScanUntil(`[`); ok {
		t.Fatal("malformed ScanUntil should miss")
	}
	if _, ok := s.Check(`(?<`); ok {
		t.Fatal("malformed Check should miss")
	}
	if n, ok := s.Match(`(`); ok || n != -1 {
		t.Fatal("malformed Match should miss")
	}
}

func TestCompileCacheHit(t *testing.T) {
	// Two scans with the same pattern source hit the compile cache the second
	// time (no recompile); behavior is unchanged.
	s := New("aa")
	if _, ok := s.Scan(`a`); !ok {
		t.Fatal("first scan a")
	}
	if _, ok := s.Scan(`a`); !ok {
		t.Fatal("second scan a (cache hit)")
	}
}

func TestErrorString(t *testing.T) {
	e := &Error{msg: "boom"}
	if e.Error() != "boom" {
		t.Fatalf("Error() = %q", e.Error())
	}
}
