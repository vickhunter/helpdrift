// Package glob matches slash-separated relative paths against glob patterns.
//
// Supported syntax:
//   - everything path.Match supports within one path segment (*, ?, [...])
//   - "**" as a whole segment matches zero or more segments
//   - {a,b} brace alternatives
//   - a pattern without "/" matches the file's base name or any directory
//     name along the path (so "node_modules" or "*.md" work anywhere)
//   - a trailing "/" means "everything below this directory"
package glob

import (
	"path"
	"strings"
)

// Match reports whether name matches pattern.
func Match(pattern, name string) bool {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	for _, p := range expandBraces(pattern) {
		if match1(p, name) {
			return true
		}
	}
	return false
}

// MatchAny reports whether name matches at least one of patterns.
func MatchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if Match(p, name) {
			return true
		}
	}
	return false
}

func match1(pattern, name string) bool {
	pattern = strings.TrimSpace(pattern)
	pattern = strings.TrimPrefix(pattern, "./")
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	if !strings.Contains(pattern, "/") {
		for _, seg := range strings.Split(name, "/") {
			if ok, _ := path.Match(pattern, seg); ok {
				return true
			}
		}
		return false
	}
	pattern = strings.TrimPrefix(pattern, "/")
	return matchSegs(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegs(p, n []string) bool {
	for len(p) > 0 {
		if p[0] == "**" {
			p = p[1:]
			if len(p) == 0 {
				return true
			}
			for i := 0; i <= len(n); i++ {
				if matchSegs(p, n[i:]) {
					return true
				}
			}
			return false
		}
		if len(n) == 0 {
			return false
		}
		ok, err := path.Match(p[0], n[0])
		if err != nil || !ok {
			return false
		}
		p, n = p[1:], n[1:]
	}
	return len(n) == 0
}

// expandBraces turns "a/{b,c}/*.{md,mdx}" into all combinations.
func expandBraces(p string) []string {
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}
	}
	depth := 0
	closeIdx := -1
	var parts []string
	last := open + 1
	for i := open; i < len(p); i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		case ',':
			if depth == 1 {
				parts = append(parts, p[last:i])
				last = i + 1
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return []string{p}
	}
	parts = append(parts, p[last:closeIdx])
	var out []string
	for _, part := range parts {
		out = append(out, expandBraces(p[:open]+part+p[closeIdx+1:])...)
	}
	return out
}
