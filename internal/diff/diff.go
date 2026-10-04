// Package diff parses unified diffs (as produced by `git diff`) into
// per-file groups of removed and added lines.
package diff

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Line is one removed or added line with its line number on its side of the
// diff (old file for removed lines, new file for added lines).
type Line struct {
	Num  int
	Text string
}

// Group is a contiguous block of changes: removed lines followed or
// interleaved by added lines, with no unchanged context line in between.
type Group struct {
	Removed []Line
	Added   []Line
}

// File is the set of changes to a single file. OldPath is empty for a newly
// added file and NewPath is empty for a deleted file.
type File struct {
	OldPath string
	NewPath string
	Binary  bool
	Groups  []Group
}

// Path returns the most useful display path for the file.
func (f File) Path() string {
	if f.NewPath != "" {
		return f.NewPath
	}
	return f.OldPath
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Parse reads a unified diff. It accepts both `git diff` output and plain
// `diff -u` output, with or without context lines.
func Parse(r io.Reader) ([]File, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var files []File
	var cur *File
	var grp *Group
	var oldLine, newLine, oldRem, newRem int
	gitHeaderOpen := false // a "diff --git" line was seen and its "---" not yet

	flushGroup := func() {
		if cur != nil && grp != nil && (len(grp.Removed) > 0 || len(grp.Added) > 0) {
			cur.Groups = append(cur.Groups, *grp)
		}
		grp = nil
	}
	startFile := func() {
		flushGroup()
		files = append(files, File{})
		cur = &files[len(files)-1]
	}

	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")

		if oldRem > 0 || newRem > 0 {
			// Inside a hunk: every line belongs to it until the counts run out.
			if line == "" {
				line = " " // some tools strip the space from empty context lines
			}
			switch line[0] {
			case ' ':
				flushGroup()
				oldLine++
				newLine++
				oldRem--
				newRem--
			case '-':
				if grp == nil {
					grp = &Group{}
				}
				grp.Removed = append(grp.Removed, Line{Num: oldLine, Text: line[1:]})
				oldLine++
				oldRem--
			case '+':
				if grp == nil {
					grp = &Group{}
				}
				grp.Added = append(grp.Added, Line{Num: newLine, Text: line[1:]})
				newLine++
				newRem--
			case '\\':
				// "\ No newline at end of file"
			default:
				return nil, fmt.Errorf("diff: unexpected line inside hunk: %q", line)
			}
			if oldRem <= 0 && newRem <= 0 {
				flushGroup()
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, "diff --git "):
			startFile()
			gitHeaderOpen = true
			a, b := splitGitHeader(strings.TrimPrefix(line, "diff --git "))
			cur.OldPath, cur.NewPath = a, b
		case strings.HasPrefix(line, "--- "):
			if !gitHeaderOpen {
				startFile()
			}
			gitHeaderOpen = false
			cur.OldPath = cleanPath(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			if cur == nil {
				startFile()
			}
			cur.NewPath = cleanPath(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch"):
			if cur != nil {
				cur.Binary = true
			}
		case strings.HasPrefix(line, "deleted file mode"):
			if cur != nil {
				cur.NewPath = ""
			}
		case strings.HasPrefix(line, "new file mode"):
			if cur != nil {
				cur.OldPath = ""
			}
		case strings.HasPrefix(line, "@@"):
			m := hunkRe.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("diff: malformed hunk header: %q", line)
			}
			if cur == nil {
				startFile()
			}
			gitHeaderOpen = false
			oldLine = atoi(m[1])
			oldRem = 1
			if m[2] != "" {
				oldRem = atoi(m[2])
			}
			newLine = atoi(m[3])
			newRem = 1
			if m[4] != "" {
				newRem = atoi(m[4])
			}
			// With zero-length sides git reports the line *before* the change.
			if oldRem == 0 {
				oldLine++
			}
			if newRem == 0 {
				newLine++
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flushGroup()
	return files, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func cleanPath(p string) string {
	if i := strings.IndexByte(p, '\t'); i >= 0 { // "--- file\t2026-01-01 ..."
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	if p == "/dev/null" {
		return ""
	}
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		if u, err := strconv.Unquote(p); err == nil {
			p = u
		}
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		p = p[2:]
	}
	return p
}

func splitGitHeader(s string) (string, string) {
	// "a/x/y.go b/x/y.go" — paths with spaces are ambiguous here, but the
	// following ---/+++ lines override these values anyway.
	if i := strings.Index(s, " b/"); i >= 0 {
		return cleanPath(s[:i]), cleanPath(s[i+1:])
	}
	parts := strings.Fields(s)
	if len(parts) == 2 {
		return cleanPath(parts[0]), cleanPath(parts[1])
	}
	return "", ""
}

// FromGit runs `git diff` in dir between base and head and parses the result.
// When head is empty the working tree is compared against base. The diff is
// taken from the merge base of base and head, so only the changes made on the
// head side are reported (like a pull request).
func FromGit(dir, base, head string) ([]File, error) {
	from := base
	h := head
	if h == "" {
		h = "HEAD"
	}
	if mb, err := Git(dir, "merge-base", base, h); err == nil {
		from = strings.TrimSpace(string(mb))
	}
	args := []string{"-c", "core.quotePath=false", "diff", "-U0", "--no-color", "--no-ext-diff", "--no-textconv", "-M", "--relative", from}
	if head != "" {
		args = append(args, head)
	}
	out, err := Git(dir, args...)
	if err != nil {
		return nil, err
	}
	return Parse(bytes.NewReader(out))
}

// Git runs a git command in dir and returns its standard output.
func Git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}
