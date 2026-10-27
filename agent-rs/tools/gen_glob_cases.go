//go:build ignore

// Generates crates/rfe-indexd/tests/data/glob_cases.tsv: pattern, name and Go's path.Match verdict, so the Rust
// port is checked against the real implementation. Run from the repo root:
//
//	go run agent-rs/tools/gen_glob_cases.go > agent-rs/crates/rfe-indexd/tests/data/glob_cases.tsv
package main

import (
	"fmt"
	"math/rand"
	"path"
	"strings"
)

func main() {
	alphabet := []string{"a", "b", "c", "A", "é", "日", ".", "-", "_", "1", " ", "x"}
	atoms := []string{"*", "?", "[a-c]", "[^a]", "[abc]", "[]a]", "[a-]", "\\*", "\\?", "\\[", "[\\]]", "[a-", "\\", "[", "]", "[!a]", "[é-日]"}
	r := rand.New(rand.NewSource(7))
	pick := func(set []string, n int) string {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteString(set[r.Intn(len(set))])
		}
		return sb.String()
	}
	seen := map[string]bool{}
	emit := func(p, n string) {
		k := p + "\x00" + n
		if seen[k] || strings.ContainsAny(p+n, "\t\n") {
			return
		}
		seen[k] = true
		ok, err := path.Match(p, n)
		verdict := "0"
		if err != nil {
			verdict = "E"
		} else if ok {
			verdict = "1"
		}
		fmt.Printf("%s\t%s\t%s\n", p, n, verdict)
	}
	fixed := [][2]string{{"*", ""}, {"*", "abc"}, {"*.txt", "a.txt"}, {"*.txt", ".txt"}, {"a*b", "ab"}, {"a*b", "axxb"}, {"a*b", "axxbc"},
		{"?", "é"}, {"??", "日本"}, {"[a-c]", "b"}, {"[^a-c]", "d"}, {"a/b", "a/b"}, {"*", "a/b"}, {"a*", "a/b"}, {"\\*", "*"}, {"IMG_*.jpg", "img_1.jpg"}}
	for _, c := range fixed {
		emit(c[0], c[1])
	}
	// Lockstep: build a pattern and a name that matches it, then sometimes break the name, so the matching
	// paths are exercised and not only the failures.
	type tok struct {
		pat  string
		name func() string
	}
	rnd := func(n int) string { return pick(alphabet, n) }
	toks := []tok{
		{"*", func() string { return rnd(r.Intn(4)) }},
		{"?", func() string { return rnd(1) }},
		{"[a-c]", func() string { return string("abc"[r.Intn(3)]) }},
		{"[^a]", func() string { return "b" }},
		{"[abc]", func() string { return "c" }},
		{"[é-日]", func() string { return "日" }},
		{"\\*", func() string { return "*" }},
		{"\\?", func() string { return "?" }},
	}
	for i := 0; i < 6000; i++ {
		var p, n strings.Builder
		for j := 0; j < 1+r.Intn(5); j++ {
			if r.Intn(2) == 0 {
				t := toks[r.Intn(len(toks))]
				p.WriteString(t.pat)
				n.WriteString(t.name())
			} else {
				l := rnd(1 + r.Intn(2))
				p.WriteString(l)
				n.WriteString(l)
			}
		}
		name := n.String()
		emit(p.String(), name)
		if rs := []rune(name); len(rs) > 0 && r.Intn(2) == 0 {
			emit(p.String(), string(rs[:len(rs)-1]))
			emit(p.String(), name+"x")
		}
	}
	for i := 0; i < 6000; i++ {
		var p strings.Builder
		for j := 0; j < 1+r.Intn(5); j++ {
			if r.Intn(3) == 0 {
				p.WriteString(atoms[r.Intn(len(atoms))])
			} else {
				p.WriteString(pick(alphabet, 1+r.Intn(2)))
			}
		}
		name := pick(alphabet, r.Intn(7))
		emit(strings.ToLower(p.String()), strings.ToLower(name))
		emit(p.String(), name)
	}
}
