package strscan

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// The oracle tests drive a wide differential corpus through both this Scanner
// and MRI's StringScanner and compare the results step by step. They self-skip
// when ruby is not on PATH (qemu / no-ruby CI lanes), where the deterministic
// tests below already cover 100%. The oracle ruby scripts set $stdout.binmode
// so Windows newline translation never corrupts the captured output (it never
// runs on Windows anyway, but the convention is kept).

func hasRuby() bool {
	_, err := exec.LookPath("ruby")
	return err == nil
}

// rubyResult runs a one-liner that builds a StringScanner over input and prints
// one line per probe; the Go side reproduces the same probe sequence.
func runRuby(t *testing.T, script, input string) string {
	t.Helper()
	// The input is fed on binmode stdin (not argv): a Windows command line would
	// CRLF-translate an embedded "\n", making ruby scan a different string than
	// the Go library and diverging on every newline-bearing case.
	cmd := exec.Command("ruby", "-rstrscan", "-e", "$stdout.binmode; "+script)
	cmd.Stdin = strings.NewReader(input)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("ruby failed: %v\nstderr: %s", err, errb.String())
	}
	return out.String()
}

// inspectStr renders a value the way the comparison protocol expects: a present
// string as its raw bytes, absence as the literal "\x00NIL". Ruby uses the same
// sentinel so a nil result is distinguishable from an empty string.
const nilSentinel = "\x00NIL"

func sv(v string, ok bool) string {
	if !ok {
		return nilSentinel
	}
	return v
}

// corpus is the differential corpus: each entry is an input string plus a
// sequence of operations (each a regex-source pattern or a directive) replayed
// identically by Go and by MRI.
type probe struct {
	op   string // "scan","scan_until","skip","skip_until","match","check","check_until","getch","peek:N","pos=N","unscan","terminate","reset","group:N","groupname:NAME"
	pat  string // pattern source for the regex ops
	args int    // numeric arg for peek/pos=/group
	name string // name for groupname
}

func TestDifferentialAgainstMRI(t *testing.T) {
	if !hasRuby() {
		t.Skip("ruby not on PATH; skipping differential test")
	}

	cases := []struct {
		input  string
		probes []probe
	}{
		// Plain anchored scans, captures, named groups.
		{"2026-06-29 rest", []probe{
			{op: "scan", pat: `(?<y>\d+)-(?<m>\d+)-(?<d>\d+)`},
			{op: "group", args: 0}, {op: "group", args: 1}, {op: "group", args: 3},
			{op: "groupname", name: "y"}, {op: "groupname", name: "m"}, {op: "groupname", name: "z"},
			{op: "matched"}, {op: "matched_size"}, {op: "pre_match"}, {op: "post_match"},
			{op: "scan", pat: ` `}, {op: "scan", pat: `rest`}, {op: "eos"},
		}},
		// scan_until + matched / pre / post / pos / charpos.
		{"foo=bar; baz=qux", []probe{
			{op: "scan_until", pat: `=`}, {op: "matched"}, {op: "pre_match"}, {op: "post_match"},
			{op: "pos"}, {op: "charpos"}, {op: "rest"},
			{op: "scan_until", pat: `;`}, {op: "matched"}, {op: "rest"},
		}},
		// check / skip family does/doesn't advance.
		{"hello world", []probe{
			{op: "check", pat: `hel`}, {op: "pos"},
			{op: "skip", pat: `hel`}, {op: "pos"},
			{op: "check_until", pat: `o`}, {op: "pos"}, {op: "matched"}, {op: "pre_match"}, {op: "post_match"},
			{op: "skip_until", pat: `o`}, {op: "pos"},
		}},
		// match? + getch + bol + eos.
		{"ab\ncd", []probe{
			{op: "match", pat: `a.`}, {op: "pos"},
			{op: "getch"}, {op: "getch"}, {op: "getch"}, {op: "bol"},
			{op: "getch"}, {op: "getch"}, {op: "eos"}, {op: "bol"},
		}},
		// Multibyte UTF-8 pos vs charpos, getch.
		{"héllo wörld", []probe{
			{op: "scan", pat: `h`}, {op: "scan", pat: `.`}, {op: "pos"}, {op: "charpos"},
			{op: "peek", args: 3}, {op: "rest"}, {op: "rest_size"},
			{op: "getch"}, {op: "getch"}, {op: "pos"}, {op: "charpos"},
		}},
		{"é日=x", []probe{
			{op: "scan_until", pat: `=`}, {op: "pos"}, {op: "charpos"}, {op: "matched"}, {op: "pre_match"},
		}},
		// unscan after scan / getch / skip; double unscan; unscan after failed scan.
		{"abcdef", []probe{
			{op: "scan", pat: `ab`}, {op: "scan", pat: `cd`}, {op: "pos"},
			{op: "unscan"}, {op: "pos"}, {op: "rest"},
			{op: "scan", pat: `cd`}, {op: "scan", pat: `zz`}, {op: "unscan"},
		}},
		{"abc", []probe{
			{op: "getch"}, {op: "unscan"}, {op: "pos"},
			{op: "skip", pat: `ab`}, {op: "unscan"}, {op: "pos"},
			{op: "unscan"}, // nothing to undo -> error
		}},
		// scan failure clears matched / matched_size / pre / post.
		{"xyz", []probe{
			{op: "scan", pat: `x`}, {op: "matched"},
			{op: "scan", pat: `zzz`}, {op: "matched"}, {op: "matched_size"},
			{op: "pre_match"}, {op: "post_match"}, {op: "group", args: 0},
		}},
		// pos= (positive, negative, boundary) and out-of-range RangeError.
		{"hello", []probe{
			{op: "pos=", args: 2}, {op: "rest"}, {op: "pos"},
			{op: "pos=", args: -2}, {op: "pos"}, {op: "rest"},
			{op: "pos=", args: 99}, // RangeError
			{op: "pos=", args: 5}, {op: "eos"},
		}},
		// empty scanner.
		{"", []probe{
			{op: "eos"}, {op: "getch"}, {op: "bol"}, {op: "rest"}, {op: "rest_size"},
			{op: "scan", pat: `x`}, {op: "scan", pat: ``}, {op: "matched"},
		}},
		// empty / wildcard matches.
		{"abc", []probe{
			{op: "scan", pat: `x*`}, {op: "pos"}, {op: "matched"}, {op: "matched_size"},
			{op: "check", pat: `a`}, {op: "scan", pat: `abc`}, {op: "eos"},
		}},
		// terminate / reset clear match.
		{"abc", []probe{
			{op: "scan", pat: `a`}, {op: "terminate"}, {op: "eos"}, {op: "matched"},
			{op: "reset"}, {op: "pos"}, {op: "matched"}, {op: "scan", pat: `abc`},
		}},
		// getch [] access: [0] is the char, [1] is nil.
		{"xy", []probe{
			{op: "getch"}, {op: "group", args: 0}, {op: "group", args: 1}, {op: "matched"},
		}},
		// peek beyond end, peek 0.
		{"a", []probe{
			{op: "getch"}, {op: "getch"}, {op: "peek", args: 5}, {op: "peek", args: 0},
		}},
		// optional / non-participating capture group.
		{"color", []probe{
			{op: "scan", pat: `colou?(r)(s)?`}, {op: "group", args: 1}, {op: "group", args: 2}, {op: "group", args: 9},
		}},
		// bol? mid-string after a non-newline (false) and at a fresh line (true).
		{"a\nb", []probe{
			{op: "scan", pat: `a`}, {op: "bol"}, {op: "scan", pat: `\n`}, {op: "bol"},
		}},
	}

	for ci, c := range cases {
		want := mriReplay(t, c.input, c.probes)
		got := goReplay(t, c.input, c.probes)
		if !equalSlices(got, want) {
			t.Errorf("case %d input=%q diverged:\n GO : %s\n MRI: %s", ci, c.input, sliceDump(got), sliceDump(want))
		}
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sliceDump(s []string) string {
	var b strings.Builder
	for i, v := range s {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(strconv.Quote(v))
	}
	return b.String()
}

// goReplay runs the probe sequence through this Scanner, producing one result
// string per probe.
func goReplay(t *testing.T, input string, probes []probe) []string {
	t.Helper()
	s := New(input)
	out := make([]string, 0, len(probes))
	for _, p := range probes {
		out = append(out, runGoProbe(s, p))
	}
	return out
}

func runGoProbe(s *Scanner, p probe) string {
	switch p.op {
	case "scan":
		return sv(s.Scan(p.pat))
	case "scan_until":
		return sv(s.ScanUntil(p.pat))
	case "skip":
		n, ok := s.Skip(p.pat)
		return sv(strconv.Itoa(n), ok)
	case "skip_until":
		n, ok := s.SkipUntil(p.pat)
		return sv(strconv.Itoa(n), ok)
	case "match":
		n, ok := s.Match(p.pat)
		return sv(strconv.Itoa(n), ok)
	case "check":
		return sv(s.Check(p.pat))
	case "check_until":
		return sv(s.CheckUntil(p.pat))
	case "getch":
		return sv(s.Getch())
	case "peek":
		return s.Peek(p.args)
	case "pos":
		return strconv.Itoa(s.Pos())
	case "charpos":
		return strconv.Itoa(s.CharPos())
	case "pos=":
		if err := s.SetPos(p.args); err != nil {
			return "RANGEERR"
		}
		return "OK"
	case "rest":
		return s.Rest()
	case "rest_size":
		return strconv.Itoa(s.RestSize())
	case "eos":
		return strconv.FormatBool(s.EOS())
	case "bol":
		return strconv.FormatBool(s.Beginning())
	case "terminate":
		s.Terminate()
		return "OK"
	case "reset":
		s.Reset()
		return "OK"
	case "matched":
		return sv(s.Matched())
	case "matched_size":
		return strconv.Itoa(s.MatchedSize())
	case "pre_match":
		return sv(s.PreMatch())
	case "post_match":
		return sv(s.PostMatch())
	case "group":
		return sv(s.Group(p.args))
	case "groupname":
		return sv(s.GroupName(p.name))
	case "unscan":
		if err := s.Unscan(); err != nil {
			return "UNSCANERR"
		}
		return "OK"
	}
	panic("unknown op " + p.op)
}

// mriReplay encodes the probe sequence as a ruby program and parses the
// newline-delimited results back, applying the same nil-sentinel protocol.
func mriReplay(t *testing.T, input string, probes []probe) []string {
	t.Helper()
	var prog strings.Builder
	// emit(v) prints a present string or the nil sentinel; emi(int_or_nil) maps
	// nil to the -1 used by Skip/Match/MatchedSize on miss; emb(bool) prints
	// true/false. RANGEERR / UNSCANERR / OK mirror the Go control results.
	// Every emitted value is base64-encoded so that results containing newlines
	// (a scanned "\n", a getch of "\n") never corrupt the one-line-per-probe
	// protocol. The nil sentinel and the -1/true/false control strings are
	// encoded the same way.
	prog.WriteString(`
require "base64"
def emit(s); $stdout.write(Base64.strict_encode64(s)); $stdout.write("\n"); end
def out(v); emit(v.nil? ? "\x00NIL" : v); end
def outi(v); emit(v.nil? ? "-1" : v.to_s); end
def outb(v); emit(v ? "true" : "false"); end
def outr(v); emit(v); end
s = StringScanner.new($stdin.binmode.read.force_encoding("UTF-8"))
`)
	for _, p := range probes {
		prog.WriteString(rubyProbe(p))
	}
	got := runRuby(t, prog.String(), input)
	sc := bufio.NewScanner(strings.NewReader(got))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var lines []string
	for sc.Scan() {
		dec, err := base64.StdEncoding.DecodeString(sc.Text())
		if err != nil {
			t.Fatalf("decoding ruby output line %q: %v", sc.Text(), err)
		}
		lines = append(lines, string(dec))
	}
	return lines
}

func rubyProbe(p probe) string {
	q := func(s string) string { return strconv.Quote(s) }
	switch p.op {
	case "scan":
		return "out(s.scan(Regexp.new(" + q(p.pat) + ")))\n"
	case "scan_until":
		return "out(s.scan_until(Regexp.new(" + q(p.pat) + ")))\n"
	case "skip":
		return "outi(s.skip(Regexp.new(" + q(p.pat) + ")))\n"
	case "skip_until":
		return "outi(s.skip_until(Regexp.new(" + q(p.pat) + ")))\n"
	case "match":
		return "outi(s.match?(Regexp.new(" + q(p.pat) + ")))\n"
	case "check":
		return "out(s.check(Regexp.new(" + q(p.pat) + ")))\n"
	case "check_until":
		return "out(s.check_until(Regexp.new(" + q(p.pat) + ")))\n"
	case "getch":
		return "out(s.getch)\n"
	case "peek":
		return "outr(s.peek(" + strconv.Itoa(p.args) + "))\n"
	case "pos":
		return "outr(s.pos.to_s)\n"
	case "charpos":
		return "outr(s.charpos.to_s)\n"
	case "pos=":
		return "begin; s.pos = " + strconv.Itoa(p.args) + "; outr(\"OK\"); rescue RangeError; outr(\"RANGEERR\"); end\n"
	case "rest":
		return "outr(s.rest)\n"
	case "rest_size":
		return "outr(s.rest_size.to_s)\n"
	case "eos":
		return "outb(s.eos?)\n"
	case "bol":
		return "outb(s.bol?)\n"
	case "terminate":
		return "s.terminate; outr(\"OK\")\n"
	case "reset":
		return "s.reset; outr(\"OK\")\n"
	case "matched":
		return "out(s.matched)\n"
	case "matched_size":
		return "outi(s.matched_size)\n"
	case "pre_match":
		return "out(s.pre_match)\n"
	case "post_match":
		return "out(s.post_match)\n"
	case "group":
		return "out(s[" + strconv.Itoa(p.args) + "])\n"
	case "groupname":
		// MRI raises IndexError for an unknown name; this Scanner returns
		// (\"\", false) instead (the Go idiom — no panic). Both map to the nil
		// sentinel here so the surface behaviors line up.
		return "begin; out(s[" + q(p.name) + ".to_sym]); rescue IndexError; out(nil); end\n"
	case "unscan":
		return "begin; s.unscan; outr(\"OK\"); rescue StringScanner::Error; outr(\"UNSCANERR\"); end\n"
	}
	panic("unknown op " + p.op)
}
