package index

import (
	"strings"

	"github.com/vickhunter/helpdrift/internal/tokens"
)

// KindSet is a bit set of token kinds.
type KindSet uint16

var kindBits = map[tokens.Kind]KindSet{
	tokens.String: 1 << 0, tokens.Text: 1 << 1, tokens.Ident: 1 << 2, tokens.Flag: 1 << 3,
	tokens.Env: 1 << 4, tokens.Path: 1 << 5, tokens.Price: 1 << 6, tokens.Number: 1 << 7,
}

// Kinds builds a KindSet.
func Kinds(ks ...tokens.Kind) KindSet {
	var s KindSet
	for _, k := range ks {
		s |= kindBits[k]
	}
	return s
}

// Has reports whether k is in the set.
func (s KindSet) Has(k tokens.Kind) bool { return s&kindBits[k] != 0 }

// Presence records, for a fixed set of candidate strings, which token kinds
// each candidate appears as in a tree. Only candidates are tracked, so memory
// stays proportional to what the docs and the diff mention.
type Presence struct {
	exact map[string]KindSet // candidate -> kinds seen
	fold  map[string]KindSet // lower(candidate) -> kinds seen (case-insensitive)
}

// NewPresence creates an index for the given candidates. Case-insensitive
// lookups are only answered for candidates passed in fold.
func NewPresence(exact, fold []string) *Presence {
	p := &Presence{exact: map[string]KindSet{}, fold: map[string]KindSet{}}
	for _, c := range exact {
		p.exact[c] = 0
	}
	for _, c := range fold {
		p.fold[strings.ToLower(c)] = 0
	}
	return p
}

// AddFile tokenizes a file and records candidate hits.
func (p *Presence) AddFile(name string, data []byte) {
	opt := tokens.OptionsFor(name)
	for _, line := range strings.Split(string(data), "\n") {
		for _, t := range tokens.Line(line, opt) {
			if _, ok := p.exact[t.Text]; ok {
				p.exact[t.Text] |= kindBits[t.Kind]
			}
			if len(p.fold) > 0 {
				low := strings.ToLower(t.Text)
				if _, ok := p.fold[low]; ok {
					p.fold[low] |= kindBits[t.Kind]
				}
			}
		}
	}
}

// Has reports whether text was seen as one of kinds. With fold set the
// comparison is case-insensitive (the candidate must have been registered
// for folding).
func (p *Presence) Has(text string, kinds KindSet, fold bool) bool {
	if p.exact[text]&kinds != 0 {
		return true
	}
	return fold && p.fold[strings.ToLower(text)]&kinds != 0
}
