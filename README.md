![go-ruby-strscan/strscan](https://raw.githubusercontent.com/go-ruby-strscan/brand/main/social/go-ruby-strscan-strscan.png)

# strscan

A pure-Go, CGO-free reimplementation of Ruby's
[`StringScanner`](https://docs.ruby-lang.org/en/master/StringScanner.html) (the
stdlib `strscan` library), matching MRI 4.0.5's observable behavior. A `Scanner`
walks a string left to right, matching regular expressions anchored at — or
searched forward from — the current scan position, advancing past each match and
recording it so the caller can read back the whole match, its capture groups, and
the surrounding text.

Pattern matching is delegated to the sibling pure-Go Onigmo engine
[`github.com/go-ruby-regexp/regexp`](https://github.com/go-ruby-regexp/regexp),
so the scanner's regex semantics — UTF-8 awareness, named groups, character
classes — match Ruby exactly.

* **Pure Go, CGO-free** — builds and runs on all six 64-bit Go targets
  (amd64, arm64, riscv64, loong64, ppc64le, s390x).
* **MRI-faithful** — verified byte-for-byte against MRI 4.0.5 across a broad
  differential corpus (anchored vs. forward search, captures, named groups,
  multibyte UTF-8, `pos`/`charpos`, `unscan`, EOS edges).
* **100% test coverage**, `gofmt`/`go vet` clean.

## Install

```sh
go get github.com/go-ruby-strscan/strscan
```

## Usage

```go
package main

import (
	"fmt"

	"github.com/go-ruby-strscan/strscan"
)

func main() {
	s := strscan.New("2026-06-29 the rest")

	date, _ := s.Scan(`(?<y>\d+)-(?<m>\d+)-(?<d>\d+)`)
	fmt.Println(date) // 2026-06-29

	y, _ := s.GroupName("y")
	m, _ := s.Group(2)
	fmt.Println(y, m) // 2026 06

	s.Scan(` `)
	word, _ := s.ScanUntil(` `) // "the "
	fmt.Printf("%q\n", word)

	fmt.Println(s.Rest()) // rest
	fmt.Println(s.EOS())  // false
}
```

## API

| Go method | Ruby `StringScanner` |
| --- | --- |
| `New(s)` | `StringScanner.new(s)` |
| `Scan(pat)` | `scan` |
| `ScanUntil(pat)` | `scan_until` |
| `Skip(pat)` | `skip` |
| `SkipUntil(pat)` | `skip_until` |
| `Match(pat)` | `match?` (returns the byte length, or -1) |
| `Check(pat)` | `check` |
| `CheckUntil(pat)` | `check_until` |
| `Peek(n)` | `peek` |
| `Getch()` | `getch` |
| `Pos()` / `SetPos(n)` | `pos` / `pos=` |
| `CharPos()` | `charpos` |
| `Rest()` / `RestSize()` | `rest` / `rest_size` |
| `EOS()` | `eos?` |
| `Beginning()` | `bol?` / `beginning_of_line?` |
| `Terminate()` / `Reset()` | `terminate` / `reset` |
| `Matched()` / `MatchedSize()` | `matched` / `matched?` / `matched_size` |
| `PreMatch()` / `PostMatch()` | `pre_match` / `post_match` |
| `Group(i)` / `GroupName(name)` | `[]` (Integer / Symbol) |
| `Unscan()` | `unscan` |
| `Concat(s)` | `<<` / `concat` |
| `String()` / `SetString(s)` | `string` / `string=` |

### Positions are byte offsets

`Pos`, `SetPos`, `Skip`, `SkipUntil`, `Match`, and `MatchedSize` all work in
**byte** offsets, exactly as MRI's `StringScanner#pos` and friends do. `CharPos`
returns the character index — the number of UTF-8 code points before the
position — the way MRI's `#charpos` does. For all-ASCII input the two coincide.

### Anchored matching

`Scan`, `Skip`, `Match`, and `Check` match the pattern **anchored at the current
position** — the pattern must match starting exactly at `Pos`. The `*Until`
variants instead search forward and match at the first position ahead where the
pattern matches. A pattern that matches only further ahead does not satisfy the
anchored methods.

## License

BSD-3-Clause. See [LICENSE](LICENSE).

## WebAssembly

Being pure Go (CGO=0), this library also compiles to **WebAssembly** — both
`GOOS=js GOARCH=wasm` (browser / Node.js) and `GOOS=wasip1 GOARCH=wasm` (WASI).
CI builds both targets on every push, alongside the six 64-bit native/qemu arches.

```sh
GOOS=js     GOARCH=wasm go build ./...   # browser / Node
GOOS=wasip1 GOARCH=wasm go build ./...   # WASI (wasmtime, wasmer, wasmedge, …)
```
