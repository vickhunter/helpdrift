// Package anchors extracts concrete, checkable claims ("anchors") from
// Markdown and MDX documents: UI labels in bold or quotes, inline code, CLI
// flags, environment variables, URL paths, config keys, prices and
// quantities. Each anchor carries its 1-based line and column.
package anchors

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vickhunter/helpdrift/internal/tokens"
)

// Kind classifies an anchor.
type Kind string

// Anchor kinds.
const (
	Label    Kind = "label"    // **Export CSV**, "Export CSV"
	Flag     Kind = "flag"     // --output
	Env      Kind = "env"      // ACME_API_KEY
	Path     Kind = "path"     // /settings/billing
	Code     Kind = "code"     // `export_format`, `Export CSV`
	Key      Kind = "key"      // a key in a YAML/TOML/INI code block
	Price    Kind = "price"    // $12, €9.99
	Quantity Kind = "quantity" // 14 days, 5 GB, 3 seats
)

// Anchor is one claim found in a document.
type Anchor struct {
	Text   string `json:"text"`
	Kind   Kind   `json:"kind"`
	Source string `json:"source"` // bold, quote, code, fence, prose, url
	Line   int    `json:"line"`
	Col    int    `json:"column"`
}

// Options tune extraction.
type Options struct {
	MinLength int      // minimum anchor length in characters (default 3)
	Ignore    []string // anchors to skip, compared case-insensitively
}

// Ignore markers that can be placed in documents (usually in HTML comments).
const (
	IgnoreLine  = "helpdrift-ignore-line"
	IgnoreStart = "helpdrift-ignore-start"
	IgnoreEnd   = "helpdrift-ignore-end"
)

// stopLabels are bold/quoted words that are almost never UI claims.
var stopLabels = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`note notes tip tips warning warnings important caution info example examples
		required optional new beta alpha deprecated experimental yes no true false todo tl;dr tldr step
		before after done not and or always never only must should may every all none`) {
		stopLabels[w] = true
	}
}

var stopCode = map[string]bool{"true": true, "false": true, "null": true, "nil": true, "none": true, "undefined": true, "yes": true, "no": true}

// Extract returns all anchors in a Markdown/MDX document. It returns no
// anchors if the front matter contains "helpdrift: ignore" (or "off").
func Extract(content []byte, opt Options) []Anchor {
	if opt.MinLength <= 0 {
		opt.MinLength = 3
	}
	ignore := map[string]bool{}
	for _, s := range opt.Ignore {
		ignore[strings.ToLower(strings.TrimSpace(s))] = true
	}
	e := &extractor{opt: opt, ignore: ignore, seen: map[string]bool{}}

	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for j := 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "---" {
				i = j + 1
				break
			}
			if k, v, ok := strings.Cut(t, ":"); ok && strings.TrimSpace(k) == "helpdrift" {
				switch strings.Trim(strings.TrimSpace(v), `"'`) {
				case "ignore", "off", "false", "skip":
					return nil
				}
			}
		}
	}

	var fence string // opening fence marker while inside a fenced block
	var fenceLang string
	inComment, ignoring := false, false
	for ; i < len(lines); i++ {
		ln, num := lines[i], i+1
		t := strings.TrimSpace(ln)

		if fence != "" {
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
				continue
			}
			if !ignoring && !strings.Contains(ln, IgnoreLine) {
				e.fenceLine(ln, num, fenceLang)
			}
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			n := 0
			for n < len(t) && t[n] == t[0] {
				n++
			}
			fence = t[:n]
			fenceLang = strings.ToLower(strings.Fields(t[n:] + " x")[0])
			continue
		}
		switch {
		case strings.Contains(ln, IgnoreStart):
			ignoring = true
			continue
		case strings.Contains(ln, IgnoreEnd):
			ignoring = false
			continue
		case ignoring || strings.Contains(ln, IgnoreLine):
			continue
		}
		// Multi-line HTML comments.
		if inComment {
			end := strings.Index(ln, "-->")
			if end < 0 {
				continue
			}
			inComment = false
			ln = strings.Repeat(" ", end+3) + ln[end+3:]
		}
		if k := strings.LastIndex(ln, "<!--"); k >= 0 && !strings.Contains(ln[k:], "-->") {
			inComment = true
			ln = ln[:k]
		}
		// MDX import/export statements.
		if strings.HasPrefix(t, "import ") || strings.HasPrefix(t, "export ") {
			continue
		}
		e.proseLine(ln, num)
	}
	sort.SliceStable(e.out, func(a, b int) bool {
		if e.out[a].Line != e.out[b].Line {
			return e.out[a].Line < e.out[b].Line
		}
		return e.out[a].Col < e.out[b].Col
	})
	return e.out
}

type extractor struct {
	opt    Options
	ignore map[string]bool
	seen   map[string]bool
	out    []Anchor
}

func (e *extractor) add(text string, k Kind, source string, line, byteCol int, full string) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) < e.opt.MinLength || e.ignore[strings.ToLower(text)] {
		return
	}
	switch k {
	case Label:
		low := strings.ToLower(text)
		if stopLabels[low] || len(text) > 60 || len(strings.Fields(text)) > 8 || !hasLetter(text) {
			return
		}
	case Code, Key:
		if stopCode[strings.ToLower(text)] || len(text) > 80 || !hasLetter(text) {
			return
		}
	}
	key := string(k) + "\x00" + text + "\x00" + strconv.Itoa(line)
	if e.seen[key] {
		return
	}
	e.seen[key] = true
	col := utf8.RuneCountInString(full[:clamp(byteCol, len(full))]) + 1
	e.out = append(e.out, Anchor{Text: text, Kind: k, Source: source, Line: line, Col: col})
}

var (
	linkRe     = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	bareURLRe  = regexp.MustCompile(`<?https?://[^\s<>()]+>?`)
	tagRe      = regexp.MustCompile(`</?[A-Za-z][^<>]*>`)
	boldRe     = regexp.MustCompile(`\*\*([^*\n]+?)\*\*|__([^_\n]+?)__`)
	quoteRe    = regexp.MustCompile(`"([^"\n]{2,60})"|“([^”\n]{2,60})”|‘([^’\n]{2,60})’|«([^»\n]{2,60})»`)
	flagRe     = regexp.MustCompile(`(?:^|[\s(\[=,|])(--[A-Za-z][A-Za-z0-9_-]*)`)
	envRe      = regexp.MustCompile(`\$?\{?\b([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)\b`)
	pathRe     = regexp.MustCompile(`(?:^|[\s(])(/[A-Za-z][A-Za-z0-9._-]*(?:/[A-Za-z0-9._:{}-]+)*/?)`)
	fencePath  = regexp.MustCompile(`(?:^|[\s'"])(/[A-Za-z][A-Za-z0-9._-]*(?:/[A-Za-z0-9._:{}-]+)+/?)`)
	urlPathRe  = regexp.MustCompile(`^https?://[A-Za-z0-9.-]+(?::\d+)?(/[^\s?#]*)`)
	priceRe    = regexp.MustCompile(`[$€£]\s?\d+(?:[.,]\d{1,2})?|\b\d+(?:[.,]\d{1,2})?\s?(?:USD|EUR|GBP)\b`)
	quantityRe = regexp.MustCompile(`\b\d+(?:[ -]|&nbsp;)(?:days?|hours?|minutes?|mins?|seconds?|secs?|weeks?|months?|years?|[KMGT]B|users?|seats?|members?|requests?|projects?|calls?|credits?|messages?)\b`)
	keyLineRe  = regexp.MustCompile(`^\s*-?\s*([A-Za-z_][A-Za-z0-9_.-]*)\s*[:=]`)

	flagOnlyRe = regexp.MustCompile(`^(--[A-Za-z][A-Za-z0-9_-]*)(?:[= ].*)?$`)
	envOnlyRe  = regexp.MustCompile(`^\$?\{?([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)\}?(?:=.*)?$`)
	pathOnlyRe = regexp.MustCompile(`^(?:(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+)?(/[A-Za-z0-9._~:{}/-]*)(?:[?#].*)?$`)
	identRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
)

// proseLine handles a normal Markdown line.
func (e *extractor) proseLine(ln string, num int) {
	work := []byte(ln)
	mask := func(a, b int) {
		for k := a; k < b && k < len(work); k++ {
			work[k] = ' '
		}
	}

	// Inline code spans.
	for _, sp := range codeSpans(ln) {
		e.codeSpan(ln[sp.cs:sp.ce], num, sp.cs, ln)
		mask(sp.start, sp.end)
	}
	// Links and images: keep link text, inspect absolute URLs, drop the target.
	for _, m := range linkRe.FindAllSubmatchIndex(work, -1) {
		target := string(work[m[4]:m[5]])
		e.urlPath(target, num, m[4], ln)
		if work[m[0]] == '!' {
			mask(m[0], m[1])
		} else {
			mask(m[3], m[1]) // "](target)"
			mask(m[0], m[0]+1)
		}
	}
	for _, m := range bareURLRe.FindAllIndex(work, -1) {
		e.urlPath(strings.Trim(string(work[m[0]:m[1]]), "<>"), num, m[0], ln)
		mask(m[0], m[1])
	}
	for _, m := range tagRe.FindAllIndex(work, -1) {
		mask(m[0], m[1])
	}
	// Prices and quantities can sit inside bold text, so look before masking.
	for _, m := range priceRe.FindAllIndex(work, -1) {
		e.add(strings.ReplaceAll(string(work[m[0]:m[1]]), " ", ""), Price, "prose", num, m[0], ln)
	}
	for _, m := range quantityRe.FindAllIndex(work, -1) {
		e.add(strings.ReplaceAll(string(work[m[0]:m[1]]), "&nbsp;", " "), Quantity, "prose", num, m[0], ln)
	}
	for _, m := range boldRe.FindAllSubmatchIndex(work, -1) {
		a, b := m[2], m[3]
		if a < 0 {
			a, b = m[4], m[5]
		}
		e.add(cleanLabel(string(work[a:b])), Label, "bold", num, a, ln)
		mask(m[0], m[1])
	}
	for _, m := range quoteRe.FindAllSubmatchIndex(work, -1) {
		for g := 2; g+1 < len(m); g += 2 {
			if m[g] >= 0 {
				e.add(cleanLabel(string(work[m[g]:m[g+1]])), Label, "quote", num, m[g], ln)
				break
			}
		}
		mask(m[0], m[1])
	}
	rest := string(work)
	for _, m := range flagRe.FindAllStringSubmatchIndex(rest, -1) {
		e.add(rest[m[2]:m[3]], Flag, "prose", num, m[2], ln)
	}
	for _, m := range envRe.FindAllStringSubmatchIndex(rest, -1) {
		e.add(rest[m[2]:m[3]], Env, "prose", num, m[2], ln)
	}
	for _, m := range pathRe.FindAllStringSubmatchIndex(rest, -1) {
		e.add(tokens.TrimPath(rest[m[2]:m[3]]), Path, "prose", num, m[2], ln)
	}
}

// codeSpan classifies the contents of an `inline code` span.
func (e *extractor) codeSpan(c string, num, col int, full string) {
	c = strings.TrimSpace(c)
	if len(c) >= 2 && (c[0] == '"' && c[len(c)-1] == '"' || c[0] == '\'' && c[len(c)-1] == '\'') {
		c = c[1 : len(c)-1]
	}
	switch {
	case flagOnlyRe.MatchString(c) && !strings.Contains(c, " --"):
		e.add(flagOnlyRe.FindStringSubmatch(c)[1], Flag, "code", num, col, full)
	case envOnlyRe.MatchString(c):
		e.add(envOnlyRe.FindStringSubmatch(c)[1], Env, "code", num, col, full)
	case strings.HasPrefix(c, "http://") || strings.HasPrefix(c, "https://"):
		e.urlPath(c, num, col, full)
	case pathOnlyRe.MatchString(c) && strings.Contains(c, "/") && len(c) > 1:
		e.add(tokens.TrimPath(pathOnlyRe.FindStringSubmatch(c)[1]), Path, "code", num, col, full)
	case identRe.MatchString(c):
		e.add(c, Code, "code", num, col, full)
	default:
		// A command line or phrase: pull flags, env vars and URLs out of it
		// and keep the whole span too (it may be a label in backticks).
		e.embedded(c, num, col, full, "code")
		if !strings.ContainsAny(c, "|&;$<>") {
			e.add(c, Code, "code", num, col, full)
		}
	}
}

func (e *extractor) embedded(c string, num, col int, full, source string) {
	for _, m := range flagRe.FindAllStringSubmatchIndex(c, -1) {
		e.add(c[m[2]:m[3]], Flag, source, num, col+m[2], full)
	}
	for _, m := range envRe.FindAllStringSubmatchIndex(c, -1) {
		e.add(c[m[2]:m[3]], Env, source, num, col+m[2], full)
	}
	for _, m := range bareURLRe.FindAllStringIndex(c, -1) {
		e.urlPath(strings.Trim(c[m[0]:m[1]], "<>\"'"), num, col+m[0], full)
	}
}

// fenceLine handles one line inside a fenced code block. Only strongly typed
// anchors are taken from code blocks.
func (e *extractor) fenceLine(ln string, num int, lang string) {
	e.embedded(ln, num, 0, ln, "fence")
	if !strings.Contains(ln, "://") {
		for _, m := range fencePath.FindAllStringSubmatchIndex(ln, -1) {
			e.add(tokens.TrimPath(ln[m[2]:m[3]]), Path, "fence", num, m[2], ln)
		}
	}
	switch lang {
	case "yaml", "yml", "toml", "ini", "properties", "conf", "cfg":
		if m := keyLineRe.FindStringSubmatchIndex(ln); m != nil {
			e.add(ln[m[2]:m[3]], Key, "fence", num, m[2], ln)
		}
	}
}

func (e *extractor) urlPath(u string, num, col int, full string) {
	m := urlPathRe.FindStringSubmatch(u)
	if m == nil {
		return
	}
	if p := tokens.TrimPath(m[1]); len(p) > 1 {
		e.add(p, Path, "url", num, col, full)
	}
}

type span struct{ start, end, cs, ce int }

// codeSpans finds `inline code` spans, honouring multi-backtick delimiters.
func codeSpans(s string) []span {
	var out []span
	for i := 0; i < len(s); {
		if s[i] != '`' || i > 0 && s[i-1] == '\\' {
			i++
			continue
		}
		n := 0
		for i+n < len(s) && s[i+n] == '`' {
			n++
		}
		found := false
		for j := i + n; j < len(s); {
			if s[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(s) && s[j+m] == '`' {
				m++
			}
			if m == n {
				out = append(out, span{start: i, end: j + m, cs: i + n, ce: j})
				i = j + m
				found = true
				break
			}
			j += m
		}
		if !found {
			i += n
		}
	}
	return out
}

func cleanLabel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'“”‘’«»")
	s = strings.TrimRight(s, ":.,;!?")
	s = strings.Trim(s, "\"'“”‘’«» ")
	return s
}

func hasLetter(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 0x7f {
			return true
		}
	}
	return false
}

func clamp(n, max int) int {
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}
