// Package config loads .helpdrift.yml. To stay dependency-free it reads a
// small, strict subset of YAML: top-level "key: value" pairs, where a value is
// a scalar, an inline list ([a, "b"]) or a block list ("- item" lines).
package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds file-level settings. Command-line flags override them.
type Config struct {
	Docs      []string // globs of documentation files
	Code      []string // globs of code files (empty: everything that is not docs)
	Exclude   []string // globs excluded from both docs and code
	Ignore    []string // anchors never reported (case-insensitive)
	MinLength int      // minimum anchor length
}

// Default values used when neither the config file nor flags set them.
var (
	DefaultDocs    = []string{"**/*.md", "**/*.mdx"}
	DefaultExclude = []string{
		".git", "node_modules", "vendor", "dist", "build", ".next", ".venv", "__pycache__",
		"CHANGELOG*", "HISTORY*", "RELEASES*", "*.lock", "package-lock.json", "go.sum",
		"pnpm-lock.yaml", "*.min.js", "*.map", "*.svg",
	}
)

// FileNames are looked up in the repository root, in order.
var FileNames = []string{".helpdrift.yml", ".helpdrift.yaml"}

// Find returns the path of the config file in root, or "" if there is none.
func Find(root string) string {
	for _, n := range FileNames {
		p := filepath.Join(root, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// Load reads and parses a config file.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	c, err := Parse(b)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse parses config file contents.
func Parse(b []byte) (Config, error) {
	var c Config
	values := map[string][]string{}
	scalars := map[string]string{}
	var listKey string

	sc := bufio.NewScanner(bytes.NewReader(b))
	n := 0
	for sc.Scan() {
		n++
		raw := strings.TrimRight(stripComment(sc.Text()), " \t")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "- ") || t == "-" {
			if listKey == "" {
				return c, fmt.Errorf("line %d: list item without a key", n)
			}
			values[listKey] = append(values[listKey], unquote(strings.TrimSpace(strings.TrimPrefix(t, "-"))))
			continue
		}
		if raw[0] == ' ' || raw[0] == '\t' {
			return c, fmt.Errorf("line %d: nested mappings are not supported", n)
		}
		k, v, ok := strings.Cut(t, ":")
		if !ok {
			return c, fmt.Errorf("line %d: expected \"key: value\"", n)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		listKey = ""
		switch {
		case v == "":
			listKey = k
			values[k] = []string{}
		case strings.HasPrefix(v, "["):
			if !strings.HasSuffix(v, "]") {
				return c, fmt.Errorf("line %d: unterminated inline list", n)
			}
			values[k] = splitList(v[1 : len(v)-1])
		default:
			scalars[k] = unquote(v)
		}
	}
	if err := sc.Err(); err != nil {
		return c, err
	}

	for k, v := range values {
		switch k {
		case "docs":
			c.Docs = v
		case "code":
			c.Code = v
		case "exclude":
			c.Exclude = v
		case "ignore":
			c.Ignore = v
		default:
			return c, fmt.Errorf("unknown list key %q", k)
		}
	}
	for k, v := range scalars {
		switch k {
		case "min_length", "min-length":
			i, err := strconv.Atoi(v)
			if err != nil || i < 1 {
				return c, fmt.Errorf("%s: want a positive integer, got %q", k, v)
			}
			c.MinLength = i
		case "docs", "code", "exclude", "ignore":
			// a single scalar is accepted as a one-element list
			list := []string{v}
			switch k {
			case "docs":
				c.Docs = list
			case "code":
				c.Code = list
			case "exclude":
				c.Exclude = list
			case "ignore":
				c.Ignore = list
			}
		default:
			return c, fmt.Errorf("unknown key %q", k)
		}
	}
	return c, nil
}

// stripComment removes a trailing "# comment" that is not inside quotes.
func stripComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i]
		}
	}
	return s
}

func splitList(s string) []string {
	var out []string
	var cur strings.Builder
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			q = c
			cur.WriteByte(c)
		case c == ',':
			out = append(out, unquote(strings.TrimSpace(cur.String())))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if last := strings.TrimSpace(cur.String()); last != "" {
		out = append(out, unquote(last))
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
