// Package index reads a repository tree (a git revision or the working
// directory) and records which candidate tokens still exist in its code.
package index

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MaxFileSize is the largest file that is read; bigger files are skipped.
const MaxFileSize = 2 << 20

// Tree is a set of files that can be listed and read.
type Tree interface {
	// Walk calls fn for every file accepted by keep, in path order. Binary and
	// oversized files are skipped.
	Walk(keep func(path string) bool, fn func(path string, data []byte) error) error
}

// FSTree is the working directory rooted at Root. Inside a git work tree it
// lists files with `git ls-files` (tracked plus untracked, honouring
// .gitignore); otherwise it walks the directory.
type FSTree struct{ Root string }

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true, "__pycache__": true}

func (t FSTree) files() ([]string, error) {
	out, err := gitOutput(t.Root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err == nil {
		var files []string
		seen := map[string]bool{}
		for _, f := range strings.Split(string(out), "\x00") {
			if f != "" && !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
		sort.Strings(files)
		return files, nil
	}
	var files []string
	err = filepath.WalkDir(t.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != t.Root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(t.Root, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

// Walk implements Tree.
func (t FSTree) Walk(keep func(string) bool, fn func(string, []byte) error) error {
	files, err := t.files()
	if err != nil {
		return err
	}
	for _, f := range files {
		if !keep(f) {
			continue
		}
		full := filepath.Join(t.Root, filepath.FromSlash(f))
		st, err := os.Stat(full)
		if err != nil || !st.Mode().IsRegular() || st.Size() > MaxFileSize {
			continue // deleted in the work tree, a symlink to a dir, or too big
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		if isBinary(data) {
			continue
		}
		if err := fn(f, data); err != nil {
			return err
		}
	}
	return nil
}

// GitTree is the tree of a git revision, read without touching the work tree.
type GitTree struct {
	Dir string
	Rev string
}

// Walk implements Tree. File contents are streamed with `git cat-file --batch`.
func (t GitTree) Walk(keep func(string) bool, fn func(string, []byte) error) error {
	out, err := gitOutput(t.Dir, "ls-tree", "-r", "-z", "--long", t.Rev)
	if err != nil {
		return err
	}
	type entry struct {
		path, oid string
	}
	var entries []entry
	for _, rec := range strings.Split(string(out), "\x00") {
		// "<mode> SP <type> SP <object> SP+ <size> TAB <path>"
		meta, p, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 4 || f[1] != "blob" || !keep(p) {
			continue
		}
		if size, err := strconv.Atoi(f[3]); err != nil || size > MaxFileSize {
			continue
		}
		entries = append(entries, entry{p, f[2]})
	}
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	cmd := exec.Command("git", "-C", t.Dir, "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		w := bufio.NewWriter(stdin)
		for _, e := range entries {
			fmt.Fprintln(w, e.oid)
		}
		w.Flush()
		stdin.Close()
	}()
	r := bufio.NewReader(stdout)
	var walkErr error
	for _, e := range entries {
		header, err := r.ReadString('\n')
		if err != nil {
			walkErr = fmt.Errorf("git cat-file: %w", err)
			break
		}
		f := strings.Fields(header)
		if len(f) != 3 {
			continue // "<oid> missing"
		}
		size, _ := strconv.Atoi(f[2])
		data := make([]byte, size+1) // contents plus trailing newline
		if _, err := io.ReadFull(r, data); err != nil {
			walkErr = fmt.Errorf("git cat-file: %w", err)
			break
		}
		data = data[:size]
		if walkErr == nil && !isBinary(data) {
			if err := fn(e.path, data); err != nil {
				walkErr = err
			}
		}
	}
	io.Copy(io.Discard, r)
	if err := cmd.Wait(); err != nil && walkErr == nil {
		walkErr = fmt.Errorf("git cat-file: %w", err)
	}
	return walkErr
}

func isBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(b[:n], 0) >= 0
}

func gitOutput(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
