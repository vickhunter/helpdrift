// Package match turns a diff into a set of removed, renamed and changed
// tokens, and matches documentation anchors against it.
package match

import (
	"sort"
	"strings"

	"github.com/vickhunter/helpdrift/internal/diff"
	"github.com/vickhunter/helpdrift/internal/tokens"
)

// Occurrence is a place in the old code where a token was removed.
type Occurrence struct {
	Kind     tokens.Kind
	File     string
	Line     int
	LineText string
}

// Rename is a token that was replaced by another token in the same position
// of a changed line.
type Rename struct {
	Kind        tokens.Kind
	Old, New    string
	OldFile     string
	OldLine     int
	OldLineText string
	NewFile     string
	NewLine     int
	NewLineText string
}

// ValueChange is a number that changed next to the same key, e.g.
// "priceMonthly: 12" -> "priceMonthly: 15".
type ValueChange struct {
	Key         string
	Old, New    string
	OldFile     string
	OldLine     int
	OldLineText string
	NewFile     string
	NewLine     int
	NewLineText string
}

// ChangeSet summarizes what a diff removed or changed.
type ChangeSet struct {
	Removed map[string][]Occurrence // token text -> where it was removed
	Renames map[string][]Rename     // old token text -> replacements
	Values  []ValueChange
	Files   int // code files that contributed changes

	fold map[string][]string // lower(text) -> texts in Removed
}

// Build analyses the diff. Files for which include returns false (docs,
// excluded paths) are ignored.
func Build(files []diff.File, include func(path string) bool) *ChangeSet {
	cs := &ChangeSet{Removed: map[string][]Occurrence{}, Renames: map[string][]Rename{}, fold: map[string][]string{}}
	for _, f := range files {
		if f.Binary || !include(f.Path()) {
			continue
		}
		oldPath, newPath := f.OldPath, f.NewPath
		if oldPath == "" {
			oldPath = newPath
		}
		if newPath == "" {
			newPath = oldPath
		}
		oldOpt, newOpt := tokens.OptionsFor(oldPath), tokens.OptionsFor(newPath)
		touched := false
		for _, g := range f.Groups {
			if len(g.Removed) == 0 {
				continue
			}
			touched = true
			rem := make([][]tokens.Token, len(g.Removed))
			for i, l := range g.Removed {
				rem[i] = tokens.Line(l.Text, oldOpt)
				for _, t := range rem[i] {
					if t.Kind == tokens.Number {
						continue
					}
					cs.addRemoved(t.Text, Occurrence{Kind: t.Kind, File: oldPath, Line: l.Num, LineText: l.Text})
				}
			}
			add := make([][]tokens.Token, len(g.Added))
			for i, l := range g.Added {
				add[i] = tokens.Line(l.Text, newOpt)
			}
			for _, p := range pairLines(rem, add) {
				ro, ad := g.Removed[p[0]], g.Added[p[1]]
				for _, pr := range pairTokens(rem[p[0]], add[p[1]]) {
					o, n := rem[p[0]][pr[0]], add[p[1]][pr[1]]
					if o.Kind == tokens.Number {
						key := precedingKey(rem[p[0]], pr[0])
						if key != "" {
							cs.Values = append(cs.Values, ValueChange{Key: key, Old: o.Text, New: n.Text,
								OldFile: oldPath, OldLine: ro.Num, OldLineText: ro.Text,
								NewFile: newPath, NewLine: ad.Num, NewLineText: ad.Text})
						}
						continue
					}
					cs.Renames[o.Text] = append(cs.Renames[o.Text], Rename{Kind: o.Kind, Old: o.Text, New: n.Text,
						OldFile: oldPath, OldLine: ro.Num, OldLineText: ro.Text,
						NewFile: newPath, NewLine: ad.Num, NewLineText: ad.Text})
				}
			}
		}
		if touched {
			cs.Files++
		}
	}
	return cs
}

func (cs *ChangeSet) addRemoved(text string, o Occurrence) {
	if _, ok := cs.Removed[text]; !ok {
		low := strings.ToLower(text)
		cs.fold[low] = append(cs.fold[low], text)
	}
	cs.Removed[text] = append(cs.Removed[text], o)
}

// Candidates returns every removed token text; these are the strings whose
// presence in the new tree must be checked.
func (cs *ChangeSet) Candidates() []string {
	out := make([]string, 0, len(cs.Removed))
	for t := range cs.Removed {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// lookup returns removed occurrences of text with an allowed kind, optionally
// matching case-insensitively. It also returns the exact removed text found.
func (cs *ChangeSet) lookup(text string, allowed func(tokens.Kind) bool, fold bool) (string, []Occurrence) {
	keys := []string{text}
	if fold {
		keys = append(keys, cs.fold[strings.ToLower(text)]...)
	}
	for _, k := range keys {
		var occ []Occurrence
		for _, o := range cs.Removed[k] {
			if allowed(o.Kind) {
				occ = append(occ, o)
			}
		}
		if len(occ) > 0 {
			return k, occ
		}
	}
	return "", nil
}

// precedingKey returns the identifier or string right before token i, which
// names the value at i (as in "price: 12" or `"limit" => 5`).
func precedingKey(ts []tokens.Token, i int) string {
	for j := i - 1; j >= 0 && j >= i-3; j-- {
		switch ts[j].Kind {
		case tokens.Ident, tokens.String:
			return ts[j].Text
		case tokens.Number:
			return ""
		}
	}
	return ""
}

// kindClass groups kinds that may replace each other in a rename.
func kindClass(k tokens.Kind) tokens.Kind {
	if k == tokens.Text {
		return tokens.String
	}
	return k
}

// skeleton is the shape of a line used to pair old and new lines: identifiers
// and flags are kept, literals are reduced to their kind.
func skeleton(ts []tokens.Token) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		switch t.Kind {
		case tokens.Ident, tokens.Flag:
			out = append(out, t.Text)
		case tokens.String, tokens.Text, tokens.Number:
			out = append(out, "\x00"+string(kindClass(t.Kind)))
		}
	}
	return out
}

func similarity(a, b []string) float64 {
	if len(a)+len(b) == 0 {
		return 1
	}
	return 2 * float64(len(lcs(a, b))) / float64(len(a)+len(b))
}

// pairLines pairs removed and added lines that look like edits of each
// other. Lines must have a similar shape (skeleton); among those, lines that
// also share more literal text win, so reordered or deleted lines in a block
// of similar lines (e.g. a JSON translation file) pair up correctly.
func pairLines(rem, add [][]tokens.Token) [][2]int {
	if len(rem) == 0 || len(add) == 0 || len(rem)*len(add) > 2500 {
		return nil
	}
	exact := func(ts []tokens.Token) []string {
		out := make([]string, len(ts))
		for i, t := range ts {
			out[i] = string(kindClass(t.Kind)) + "\x00" + t.Text
		}
		return out
	}
	rs, as := make([][]string, len(rem)), make([][]string, len(add))
	re, ae := make([][]string, len(rem)), make([][]string, len(add))
	for i, t := range rem {
		rs[i], re[i] = skeleton(t), exact(t)
	}
	for i, t := range add {
		as[i], ae[i] = skeleton(t), exact(t)
	}
	type cand struct {
		i, j int
		s    float64
	}
	var cands []cand
	for i := range rs {
		for j := range as {
			shape := similarity(rs[i], as[j])
			if shape < 0.5 {
				continue
			}
			cands = append(cands, cand{i, j, shape + similarity(re[i], ae[j])})
		}
	}
	// Stable sort keeps (i, j) order among equal scores, so ties pair in order.
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].s > cands[b].s })
	usedR, usedA := map[int]bool{}, map[int]bool{}
	var pairs [][2]int
	for _, c := range cands {
		if usedR[c.i] || usedA[c.j] {
			continue
		}
		usedR[c.i], usedA[c.j] = true, true
		pairs = append(pairs, [2]int{c.i, c.j})
	}
	sort.Slice(pairs, func(a, b int) bool { return pairs[a][0] < pairs[b][0] })
	return pairs
}

// pairTokens aligns the tokens of an old and a new line and returns the
// index pairs of tokens that were replaced (old != new).
func pairTokens(old, new []tokens.Token) [][2]int {
	if len(old)*len(new) > 250000 {
		return nil
	}
	key := func(t tokens.Token) string { return string(kindClass(t.Kind)) + "\x00" + t.Text }
	a, b := make([]string, len(old)), make([]string, len(new))
	for i, t := range old {
		a[i] = key(t)
	}
	for i, t := range new {
		b[i] = key(t)
	}
	common := lcs(a, b)
	var out [][2]int
	i, j := 0, 0
	gap := func(i0, i1, j0, j1 int) {
		n, m := i1-i0, j1-j0
		if n == 0 || m == 0 {
			return
		}
		if n == m {
			for k := 0; k < n; k++ {
				if kindClass(old[i0+k].Kind) == kindClass(new[j0+k].Kind) {
					out = append(out, [2]int{i0 + k, j0 + k})
				}
			}
			return
		}
		// Unequal gap: pair a kind only when it occurs exactly once per side.
		count := func(ts []tokens.Token, lo, hi int) map[tokens.Kind][]int {
			m := map[tokens.Kind][]int{}
			for k := lo; k < hi; k++ {
				c := kindClass(ts[k].Kind)
				m[c] = append(m[c], k)
			}
			return m
		}
		oc, nc := count(old, i0, i1), count(new, j0, j1)
		for k, os := range oc {
			if ns := nc[k]; len(os) == 1 && len(ns) == 1 {
				out = append(out, [2]int{os[0], ns[0]})
			}
		}
	}
	for _, c := range common {
		gap(i, c[0], j, c[1])
		i, j = c[0]+1, c[1]+1
	}
	gap(i, len(old), j, len(new))
	sort.Slice(out, func(x, y int) bool { return out[x][0] < out[y][0] })
	return out
}

// lcs returns index pairs of a longest common subsequence of a and b.
func lcs(a, b []string) [][2]int {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return nil
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out [][2]int
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			out = append(out, [2]int{i, j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}
