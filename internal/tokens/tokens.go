// Package tokens splits a line of source code into the pieces that user
// facing documentation tends to mention: string literals, text nodes in
// markup, identifiers, CLI flags, environment variables, URL paths, prices
// and numbers.
//
// It is a deliberately simple, language-agnostic, line-based scanner. It does
// not try to parse any language; it only needs to be consistent between the
// old and new side of a diff and the current tree.
package tokens

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Kind classifies a token.
type Kind string

// Token kinds.
const (
	String Kind = "string" // contents of a quoted literal
	Text   Kind = "text"   // a text node in markup (JSX, HTML, Vue, ...)
	Ident  Kind = "ident"  // identifier or bare config key
	Flag   Kind = "flag"   // --long-flag
	Env    Kind = "env"    // UPPER_SNAKE_CASE name found inside a literal
	Path   Kind = "path"   // /url/path found inside a literal or URL
	Price  Kind = "price"  // $12, €9.99 found inside a literal or text
	Number Kind = "number" // a bare numeric literal
)

// Token is one piece of a line. Col is the 0-based byte offset.
type Token struct {
	Text string
	Kind Kind
	Col  int
}

// Options control comment handling and markup text detection.
type Options struct {
	Markup        bool // file can contain text nodes between tags
	SlashComments bool // "//" starts a comment
	HashComments  bool // "#" starts a comment
}

var (
	markupExt = set(".tsx", ".jsx", ".vue", ".svelte", ".html", ".htm", ".astro", ".erb", ".hbs", ".handlebars", ".ejs", ".liquid", ".njk", ".twig", ".php", ".cshtml", ".razor")
	slashExt  = set(".go", ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts", ".tsx", ".jsx", ".java", ".kt", ".kts", ".swift", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".rs", ".scala", ".dart", ".php", ".vue", ".svelte", ".astro", ".groovy", ".zig", ".proto", ".jsonc", ".json5")
	hashExt   = set(".py", ".rb", ".sh", ".bash", ".zsh", ".fish", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".env", ".tf", ".hcl", ".r", ".pl", ".pm", ".ex", ".exs", ".cr", ".nim", ".ps1", ".properties", ".mk", ".dockerfile", ".gitignore")
	hashNames = set("Makefile", "Dockerfile", "Gemfile", "Rakefile", "Procfile", "Justfile", "Brewfile")
)

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// OptionsFor picks Options from a file name.
func OptionsFor(name string) Options {
	base := path.Base(name)
	ext := strings.ToLower(path.Ext(base))
	if strings.HasPrefix(base, ".env") {
		ext = ".env"
	}
	return Options{
		Markup:        markupExt[ext],
		SlashComments: slashExt[ext],
		HashComments:  hashExt[ext] || hashNames[base],
	}
}

var jsKeywords = set("import", "export", "return", "const", "let", "var", "if", "else", "for", "while", "function", "class", "interface", "type", "async", "await", "case", "default", "switch", "break", "continue", "throw", "try", "catch", "finally", "new", "delete", "from", "package", "func", "def", "end", "public", "private", "protected", "static")

// Line tokenizes one line of code. Tokens are ordered by column.
func Line(s string, o Options) []Token {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return nil
	}
	if strings.HasPrefix(trim, "/*") || strings.HasPrefix(trim, "*/") || trim == "*" || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "<!--") {
		return nil
	}
	if o.SlashComments && strings.HasPrefix(trim, "//") {
		return nil
	}
	if o.HashComments && strings.HasPrefix(trim, "#") {
		return nil
	}
	if o.Markup && isTextLine(trim) {
		col := strings.Index(s, trim)
		out := []Token{{Text: trim, Kind: Text, Col: col}}
		return append(out, subTokens(trim, col)...)
	}

	var out []Token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"' || c == '\'' || c == '`':
			end, val, ok := readString(s, i)
			if !ok {
				i++
				continue
			}
			if v := strings.TrimSpace(val); v != "" {
				out = append(out, Token{Text: v, Kind: String, Col: i})
				out = append(out, subTokens(v, i+1)...)
			}
			i = end
		case o.SlashComments && c == '/' && i+1 < len(s) && s[i+1] == '/' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			i = len(s)
		case o.HashComments && c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			i = len(s)
		case c == '-' && i+2 < len(s) && s[i+1] == '-' && isLetter(s[i+2]) && (i == 0 || !isIdentByte(s[i-1]) && s[i-1] != '-'):
			j := i + 2
			for j < len(s) && (isIdentByte(s[j]) || s[j] == '-') {
				j++
			}
			out = append(out, Token{Text: s[i:j], Kind: Flag, Col: i})
			i = j
		case isDigit(c) && (i == 0 || !isIdentByte(s[i-1])):
			j := i + 1
			for j < len(s) && (isDigit(s[j]) || s[j] == '_' || s[j] == '.' && j+1 < len(s) && isDigit(s[j+1])) {
				j++
			}
			out = append(out, Token{Text: strings.ReplaceAll(s[i:j], "_", ""), Kind: Number, Col: i})
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < len(s) && (isIdentByte(s[j]) || s[j] == '-' && j+1 < len(s) && isLetter(s[j+1]) && isAlnum(s[j-1])) {
				j++
			}
			out = append(out, Token{Text: s[i:j], Kind: Ident, Col: i})
			i = j
		default:
			i++
		}
	}
	if o.Markup {
		if nodes := textNodes(s); len(nodes) > 0 {
			out = dropCovered(out, nodes)
			out = append(out, nodes...)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Col < out[b].Col })
	return out
}

// readString reads a quoted literal starting at s[i]. It returns the index
// just past the closing quote and the raw contents.
func readString(s string, i int) (int, string, bool) {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case q:
			return j + 1, s[i+1 : j], true
		}
	}
	return 0, "", false
}

// dropCovered removes scanner tokens that fall inside a text node, so the
// words of a label do not show up a second time as identifiers.
func dropCovered(ts, nodes []Token) []Token {
	out := ts[:0]
	for _, t := range ts {
		covered := false
		for _, n := range nodes {
			if n.Kind == Text && t.Col >= n.Col && t.Col < n.Col+len(n.Text) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, t)
		}
	}
	return out
}

// textNodes finds ">Some text<" segments inside a markup line.
func textNodes(s string) []Token {
	var out []Token
	for i := 0; i < len(s); i++ {
		if s[i] != '>' || i > 0 && (s[i-1] == '=' || s[i-1] == '-') {
			continue
		}
		j := i + 1
		for j < len(s) && s[j] != '<' && s[j] != '>' && s[j] != '{' && s[j] != '}' {
			j++
		}
		if j >= len(s) || s[j] != '<' || j+1 >= len(s) || !(s[j+1] == '/' || isLetter(s[j+1])) {
			continue
		}
		raw := s[i+1 : j]
		t := strings.TrimSpace(raw)
		if t == "" || !hasLetter(t) || strings.ContainsAny(t, "=;") || strings.Contains(t, "&&") || strings.Contains(t, "||") || len(t) > 120 {
			continue
		}
		col := i + 1 + strings.Index(raw, t)
		out = append(out, Token{Text: t, Kind: Text, Col: col})
		out = append(out, subTokens(t, col)...)
	}
	return out
}

// isTextLine reports whether a trimmed markup line looks like a bare text node
// (a label on its own line between an opening and closing tag).
func isTextLine(t string) bool {
	if !hasLetter(t) || strings.ContainsAny(t, "<>{}=;\"`") {
		return false
	}
	switch t[len(t)-1] {
	case ',', '(', '[', ':', '.', '\\':
		if t[len(t)-1] != '.' || !strings.Contains(t, " ") {
			return false
		}
	}
	first := t
	if i := strings.IndexAny(t, " ("); i >= 0 {
		first = t[:i]
	}
	if jsKeywords[first] {
		return false
	}
	if strings.Contains(t, "(") && !strings.Contains(t, " ") {
		return false // looks like a call: foo(bar)
	}
	return strings.Contains(t, " ") || isUpper(t[0])
}

var (
	subFlagRe  = regexp.MustCompile(`(?:^|[\s=\[(,|"'])(--[A-Za-z][A-Za-z0-9_-]*)`)
	subEnvRe   = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b`)
	urlRe      = regexp.MustCompile(`https?://[A-Za-z0-9.-]+(?::\d+)?(/[A-Za-z0-9._~%:{}/-]*)`)
	subPathRe  = regexp.MustCompile(`(?:^|[\s(])(/[A-Za-z][A-Za-z0-9._-]*(?:/[A-Za-z0-9._:{}-]+)*/?)`)
	subPriceRe = regexp.MustCompile(`[$€£]\s?\d+(?:[.,]\d{1,2})?`)
)

// subTokens pulls flags, env vars, paths and prices out of a literal's text.
func subTokens(v string, base int) []Token {
	var out []Token
	seen := map[string]bool{v: true}
	add := func(text string, k Kind, col int) {
		if text == "" || seen[text] {
			return
		}
		seen[text] = true
		out = append(out, Token{Text: text, Kind: k, Col: base + col})
	}
	for _, m := range subFlagRe.FindAllStringSubmatchIndex(v, -1) {
		add(v[m[2]:m[3]], Flag, m[2])
	}
	for _, m := range subEnvRe.FindAllStringIndex(v, -1) {
		add(v[m[0]:m[1]], Env, m[0])
	}
	for _, m := range urlRe.FindAllStringSubmatchIndex(v, -1) {
		if p := TrimPath(v[m[2]:m[3]]); len(p) > 1 {
			add(p, Path, m[2])
		}
	}
	if !strings.Contains(v, "://") {
		for _, m := range subPathRe.FindAllStringSubmatchIndex(v, -1) {
			add(TrimPath(v[m[2]:m[3]]), Path, m[2])
		}
	}
	for _, m := range subPriceRe.FindAllStringIndex(v, -1) {
		add(strings.ReplaceAll(v[m[0]:m[1]], " ", ""), Price, m[0])
	}
	return out
}

// TrimPath normalises a URL path: strips query, fragment and trailing slash.
func TrimPath(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimRight(p, ".,;:")
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func isLetter(c byte) bool     { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isUpper(c byte) bool      { return c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isAlnum(c byte) bool      { return isLetter(c) || isDigit(c) }
func isIdentStart(c byte) bool { return isLetter(c) || c == '_' || c >= 0x80 }
func isIdentByte(c byte) bool  { return isAlnum(c) || c == '_' || c >= 0x80 }

func hasLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		if isLetter(s[i]) || s[i] >= 0x80 {
			return true
		}
	}
	return false
}
