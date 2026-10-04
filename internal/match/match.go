package match

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vickhunter/helpdrift/internal/anchors"
	"github.com/vickhunter/helpdrift/internal/index"
	"github.com/vickhunter/helpdrift/internal/tokens"
)

// Change kinds reported in findings.
const (
	Renamed = "renamed" // the code replaced the claim with something else
	Removed = "removed" // the code no longer contains the claim anywhere
	Changed = "changed" // a number/price next to the same key changed
	Missing = "missing" // scan mode: the claim is not found in the code at all
)

// Evidence points at the code that triggered a finding.
type Evidence struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	Side        string `json:"side"` // "base" (old code) or "head" (new code)
	Old         string `json:"old,omitempty"`
	New         string `json:"new,omitempty"`
	Key         string `json:"key,omitempty"`
	OldFile     string `json:"old_file,omitempty"`
	OldLine     int    `json:"old_line,omitempty"`
	OldLineText string `json:"old_line_text,omitempty"`
	NewLineText string `json:"new_line_text,omitempty"`
}

// Finding is a documentation claim that the code no longer supports.
type Finding struct {
	File       string         `json:"file"`
	Line       int            `json:"line"`
	Column     int            `json:"column"`
	Anchor     anchors.Anchor `json:"anchor"`
	Change     string         `json:"change"`
	Code       *Evidence      `json:"code,omitempty"`
	Suggestion string         `json:"suggestion,omitempty"`
	Replace    *Replacement   `json:"replace,omitempty"`
	Review     *Review        `json:"llm,omitempty"`

	DocLines  []string `json:"-"` // a few lines of the document around Line
	DocOffset int      `json:"-"` // line number of DocLines[0]
}

// Replacement is a mechanical find-and-replace fix for the document.
type Replacement struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Review is the result of the optional LLM pass.
type Review struct {
	Verdict     string `json:"verdict"` // confirm, reject or error
	Reason      string `json:"reason,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	Model       string `json:"model,omitempty"`
}

// variant is one spelling under which an anchor may appear in code.
type variant struct {
	text   string
	prefix string // re-added to suggestions (e.g. "--" for bare flag names)
	kinds  index.KindSet
}

var (
	labelKinds = index.Kinds(tokens.String, tokens.Text)
	codeKinds  = index.Kinds(tokens.String, tokens.Text, tokens.Ident, tokens.Flag, tokens.Env, tokens.Path)
)

// variants lists how an anchor can be spelled in code, with the token kinds
// that count as the same thing.
func variants(a anchors.Anchor) []variant {
	switch a.Kind {
	case anchors.Label:
		return []variant{{text: a.Text, kinds: labelKinds}}
	case anchors.Flag:
		v := []variant{{text: a.Text, kinds: index.Kinds(tokens.Flag, tokens.String)}}
		if bare := strings.TrimLeft(a.Text, "-"); len(bare) >= 2 {
			v = append(v, variant{text: bare, prefix: a.Text[:len(a.Text)-len(bare)], kinds: index.Kinds(tokens.String)})
		}
		return v
	case anchors.Env:
		return []variant{{text: a.Text, kinds: index.Kinds(tokens.Env, tokens.Ident, tokens.String)}}
	case anchors.Path:
		return []variant{{text: a.Text, kinds: index.Kinds(tokens.Path, tokens.String)}}
	case anchors.Price:
		return []variant{{text: a.Text, kinds: index.Kinds(tokens.Price, tokens.String)}}
	case anchors.Quantity:
		return []variant{{text: a.Text, kinds: index.Kinds(tokens.String, tokens.Text)}}
	default: // Code, Key
		return []variant{{text: a.Text, kinds: codeKinds}}
	}
}

// PresenceCandidates returns the strings whose presence in the new tree must
// be known to match these anchors (exact and case-insensitive).
func PresenceCandidates(as []anchors.Anchor) (exact, fold []string) {
	for _, a := range as {
		for _, v := range variants(a) {
			exact = append(exact, v.text)
			if a.Kind == anchors.Label {
				fold = append(fold, v.text)
			}
		}
	}
	return exact, fold
}

// Present reports whether any spelling of the anchor exists in the tree.
func Present(a anchors.Anchor, p *index.Presence) bool {
	for _, v := range variants(a) {
		if p.Has(v.text, v.kinds, a.Kind == anchors.Label) {
			return true
		}
	}
	return false
}

// Match checks one anchor against the change set. present describes the new
// tree. It returns false when the anchor is unaffected.
func (cs *ChangeSet) Match(a anchors.Anchor, present *index.Presence) (Finding, bool) {
	f := Finding{Anchor: a, Line: a.Line, Column: a.Col}
	for _, v := range variants(a) {
		removedText, occ := cs.lookup(v.text, v.kinds.Has, a.Kind == anchors.Label)
		if occ == nil {
			continue
		}
		if Present(a, present) {
			return f, false // still in the code somewhere: the doc is fine
		}
		if r, ok := cs.bestRename(removedText, v.kinds); ok {
			to := v.prefix + r.New
			f.Change = Renamed
			f.Code = &Evidence{File: r.NewFile, Line: r.NewLine, Side: "head", Old: r.Old, New: r.New,
				OldFile: r.OldFile, OldLine: r.OldLine, OldLineText: r.OldLineText, NewLineText: r.NewLineText}
			f.Replace = &Replacement{From: a.Text, To: to}
			f.Suggestion = "replace " + strconv.Quote(a.Text) + " with " + strconv.Quote(to)
			return f, true
		}
		o := occ[0]
		f.Change = Removed
		f.Code = &Evidence{File: o.File, Line: o.Line, Side: "base", Old: removedText, OldLineText: o.LineText}
		f.Suggestion = "no longer found in the code; update or remove this mention"
		return f, true
	}
	if a.Kind == anchors.Price || a.Kind == anchors.Quantity {
		return cs.matchValue(a, f)
	}
	return f, false
}

// bestRename picks the most common replacement for a removed token.
func (cs *ChangeSet) bestRename(old string, kinds index.KindSet) (Rename, bool) {
	counts := map[string]int{}
	first := map[string]Rename{}
	for _, r := range cs.Renames[old] {
		if !kinds.Has(r.Kind) || r.New == "" || r.New == old {
			continue
		}
		if _, ok := first[r.New]; !ok {
			first[r.New] = r
		}
		counts[r.New]++
	}
	if len(counts) == 0 {
		return Rename{}, false
	}
	news := make([]string, 0, len(counts))
	for n := range counts {
		news = append(news, n)
	}
	sort.Slice(news, func(i, j int) bool {
		if counts[news[i]] != counts[news[j]] {
			return counts[news[i]] > counts[news[j]]
		}
		return news[i] < news[j]
	})
	return first[news[0]], true
}

var (
	numRe  = regexp.MustCompile(`\d+(?:[.,]\d{1,2})?`)
	unitRe = regexp.MustCompile(`[A-Za-z]+$`)
)

// unitHints maps a quantity unit to substrings expected in the code key.
var unitHints = map[string][]string{
	"day": {"day"}, "hour": {"hour"}, "minute": {"minute", "min"}, "min": {"min"},
	"second": {"second", "sec"}, "sec": {"sec"}, "week": {"week"}, "month": {"month"}, "year": {"year"},
	"kb": {"kb", "size"}, "mb": {"mb", "size"}, "gb": {"gb", "size", "storage"}, "tb": {"tb", "size", "storage"},
	"user": {"user", "seat", "member"}, "seat": {"seat", "user", "member"}, "member": {"member", "seat", "user"},
	"request": {"request", "rate"}, "project": {"project"}, "call": {"call", "request"},
	"credit": {"credit"}, "message": {"message", "msg"},
}

var priceHints = []string{"price", "amount", "cost", "fee", "monthly", "yearly", "annual", "usd", "eur", "gbp", "cents"}

// matchValue matches a price or quantity anchor against changed numbers.
func (cs *ChangeSet) matchValue(a anchors.Anchor, f Finding) (Finding, bool) {
	numText := numRe.FindString(a.Text)
	if numText == "" {
		return f, false
	}
	n := parseNum(numText)
	var hints []string
	if a.Kind == anchors.Price {
		hints = priceHints
	} else {
		u := strings.ToLower(unitRe.FindString(a.Text))
		u = strings.TrimSuffix(u, "s")
		hints = unitHints[u]
	}
	for _, vc := range cs.Values {
		key := strings.ToLower(vc.Key)
		if !containsAny(key, hints) {
			continue
		}
		old, nw := parseNum(vc.Old), parseNum(vc.New)
		scale := 1.0
		if a.Kind == anchors.Price && strings.Contains(key, "cents") {
			scale = 100
		}
		if old != n*scale || old == nw {
			continue
		}
		newNum := formatNum(nw/scale, numText)
		to := strings.Replace(a.Text, numText, newNum, 1)
		f.Change = Changed
		f.Code = &Evidence{File: vc.NewFile, Line: vc.NewLine, Side: "head", Key: vc.Key, Old: vc.Old, New: vc.New,
			OldFile: vc.OldFile, OldLine: vc.OldLine, OldLineText: vc.OldLineText, NewLineText: vc.NewLineText}
		f.Replace = &Replacement{From: a.Text, To: to}
		f.Suggestion = "replace " + strconv.Quote(a.Text) + " with " + strconv.Quote(to)
		return f, true
	}
	return f, false
}

func containsAny(s string, subs []string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func parseNum(s string) float64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil {
		return -1
	}
	return v
}

// formatNum prints v in the style of the original text (decimals, comma).
func formatNum(v float64, like string) string {
	dec := 0
	sep := "."
	if i := strings.IndexAny(like, ".,"); i >= 0 {
		dec = len(like) - i - 1
		sep = like[i : i+1]
	}
	if dec == 0 && v != float64(int64(v)) {
		dec = 2
	}
	s := strconv.FormatFloat(v, 'f', dec, 64)
	return strings.Replace(s, ".", sep, 1)
}
