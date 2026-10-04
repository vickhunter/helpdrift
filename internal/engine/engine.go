// Package engine wires the pieces together: it reads the diff and the trees,
// extracts anchors from the docs and produces findings.
package engine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vickhunter/helpdrift/internal/anchors"
	"github.com/vickhunter/helpdrift/internal/config"
	"github.com/vickhunter/helpdrift/internal/diff"
	"github.com/vickhunter/helpdrift/internal/glob"
	"github.com/vickhunter/helpdrift/internal/index"
	"github.com/vickhunter/helpdrift/internal/match"
)

// Options configure a run. Empty list fields fall back to defaults.
type Options struct {
	Root      string // repository root (default ".")
	Base      string // check: git ref to compare against
	Head      string // check/scan: git ref of the new code; empty = working tree
	DiffFile  string // check: read a unified diff from this file ("-" = Stdin) instead of git
	Strip     int    // diff file: leading path components to strip; <0 = auto-detect
	Stdin     io.Reader
	Docs      []string // doc globs
	Code      []string // code globs; empty = every non-doc file
	Exclude   []string // globs excluded from docs and code
	Ignore    []string // anchors to ignore
	MinLength int
	Kinds     []anchors.Kind // scan: anchor kinds to audit
}

// Stats describe what was examined.
type Stats struct {
	Docs         int `json:"docs"`
	Anchors      int `json:"anchors"`
	ChangedFiles int `json:"changed_code_files"`
	CodeFiles    int `json:"code_files"`
}

// Result is the outcome of a run.
type Result struct {
	Mode     string          `json:"mode"`
	Base     string          `json:"base,omitempty"`
	Head     string          `json:"head,omitempty"`
	Stats    Stats           `json:"stats"`
	Findings []match.Finding `json:"findings"`
}

// DefaultScanKinds are audited by Scan unless Options.Kinds is set. They are
// the anchor kinds that almost always refer to something in the code.
var DefaultScanKinds = []anchors.Kind{anchors.Flag, anchors.Env}

// docLikeExt are never treated as code even when they are not in the docs globs.
var docLikeExt = map[string]bool{".md": true, ".mdx": true, ".markdown": true, ".rst": true, ".adoc": true, ".txt": true}

type filters struct{ o Options }

func (f filters) isDoc(p string) bool {
	return glob.MatchAny(f.o.Docs, p) && !glob.MatchAny(f.o.Exclude, p)
}

func (f filters) isCode(p string) bool {
	if docLikeExt[strings.ToLower(path.Ext(p))] || glob.MatchAny(f.o.Docs, p) || glob.MatchAny(f.o.Exclude, p) {
		return false
	}
	return len(f.o.Code) == 0 || glob.MatchAny(f.o.Code, p)
}

func (o *Options) normalize() {
	if o.Root == "" {
		o.Root = "."
	}
	if len(o.Docs) == 0 {
		o.Docs = config.DefaultDocs
	}
	o.Exclude = append(append([]string{}, config.DefaultExclude...), o.Exclude...)
	if o.MinLength <= 0 {
		o.MinLength = 3
	}
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
}

type doc struct {
	path    string
	lines   []string
	anchors []anchors.Anchor
}

func (o Options) tree() index.Tree {
	if o.Head != "" && o.DiffFile == "" {
		return index.GitTree{Dir: o.Root, Rev: o.Head}
	}
	return index.FSTree{Root: o.Root}
}

func (o Options) readDocs(t index.Tree, f filters) ([]doc, int, error) {
	var docs []doc
	n := 0
	err := t.Walk(f.isDoc, func(p string, data []byte) error {
		as := anchors.Extract(data, anchors.Options{MinLength: o.MinLength, Ignore: o.Ignore})
		n += len(as)
		docs = append(docs, doc{path: p, lines: strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), anchors: as})
		return nil
	})
	return docs, n, err
}

func (o Options) presence(t index.Tree, f filters, exact, fold []string) (*index.Presence, int, error) {
	p := index.NewPresence(exact, fold)
	n := 0
	err := t.Walk(f.isCode, func(path string, data []byte) error {
		n++
		p.AddFile(path, data)
		return nil
	})
	return p, n, err
}

// Check compares the docs in the new tree against the code changes between
// Base and Head (or in DiffFile).
func Check(o Options) (*Result, error) {
	o.normalize()
	f := filters{o}

	var files []diff.File
	var err error
	switch {
	case o.DiffFile == "-":
		files, err = diff.Parse(o.Stdin)
	case o.DiffFile != "":
		var fh *os.File
		if fh, err = os.Open(o.DiffFile); err == nil {
			files, err = diff.Parse(fh)
			fh.Close()
		}
	case o.Base == "":
		err = errors.New("check needs --base <git ref> or --diff <file>")
	default:
		files, err = diff.FromGit(o.Root, o.Base, o.Head)
	}
	if err != nil {
		return nil, err
	}
	if o.DiffFile != "" {
		stripPaths(files, o.Root, o.Strip)
	}

	cs := match.Build(files, f.isCode)
	res := &Result{Mode: "check", Base: o.Base, Head: o.Head, Findings: []match.Finding{}}
	if o.DiffFile != "" {
		res.Base = ""
		res.Head = ""
	}
	res.Stats.ChangedFiles = cs.Files

	t := o.tree()
	docs, nAnchors, err := o.readDocs(t, f)
	if err != nil {
		return nil, fmt.Errorf("reading docs: %w", err)
	}
	res.Stats.Docs, res.Stats.Anchors = len(docs), nAnchors
	if len(cs.Removed) == 0 {
		return res, nil
	}

	// Only anchors that mention something the diff removed need a presence
	// check; collect their spellings.
	var all []anchors.Anchor
	for _, d := range docs {
		all = append(all, d.anchors...)
	}
	exact, fold := match.PresenceCandidates(all)
	pres, nCode, err := o.presence(t, f, exact, fold)
	if err != nil {
		return nil, fmt.Errorf("indexing code: %w", err)
	}
	res.Stats.CodeFiles = nCode

	for _, d := range docs {
		for _, a := range d.anchors {
			if fd, ok := cs.Match(a, pres); ok {
				fd.File = d.path
				attachContext(&fd, d.lines)
				res.Findings = append(res.Findings, fd)
			}
		}
	}
	res.Findings = dedupe(res.Findings)
	return res, nil
}

// Scan audits every doc against the current code, without a diff. It reports
// anchors of the selected kinds that do not appear anywhere in the code.
func Scan(o Options) (*Result, error) {
	o.normalize()
	if len(o.Kinds) == 0 {
		o.Kinds = DefaultScanKinds
	}
	want := map[anchors.Kind]bool{}
	for _, k := range o.Kinds {
		want[k] = true
	}
	f := filters{o}
	t := o.tree()
	docs, nAnchors, err := o.readDocs(t, f)
	if err != nil {
		return nil, fmt.Errorf("reading docs: %w", err)
	}
	res := &Result{Mode: "scan", Head: o.Head, Findings: []match.Finding{}}
	res.Stats.Docs, res.Stats.Anchors = len(docs), nAnchors

	var sel []anchors.Anchor
	for _, d := range docs {
		for _, a := range d.anchors {
			if want[a.Kind] {
				sel = append(sel, a)
			}
		}
	}
	exact, fold := match.PresenceCandidates(sel)
	pres, nCode, err := o.presence(t, f, exact, fold)
	if err != nil {
		return nil, fmt.Errorf("indexing code: %w", err)
	}
	res.Stats.CodeFiles = nCode
	for _, d := range docs {
		for _, a := range d.anchors {
			if !want[a.Kind] || match.Present(a, pres) {
				continue
			}
			fd := match.Finding{File: d.path, Line: a.Line, Column: a.Col, Anchor: a, Change: match.Missing,
				Suggestion: "not found in the code; check that it still exists"}
			attachContext(&fd, d.lines)
			res.Findings = append(res.Findings, fd)
		}
	}
	res.Findings = dedupe(res.Findings)
	return res, nil
}

// stripPaths removes leading path components from diff paths (like
// `patch -pN`). With strip < 0 the count is guessed: the smallest one that
// makes the new paths exist under root.
func stripPaths(files []diff.File, root string, strip int) {
	if strip < 0 {
		strip = 0
	guess:
		for k := 0; k <= 3; k++ {
			for _, f := range files {
				if f.NewPath == "" {
					continue
				}
				if p, ok := stripN(f.NewPath, k); ok {
					if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
						strip = k
						break guess
					}
				}
			}
		}
	}
	if strip == 0 {
		return
	}
	for i := range files {
		if p, ok := stripN(files[i].OldPath, strip); ok {
			files[i].OldPath = p
		}
		if p, ok := stripN(files[i].NewPath, strip); ok {
			files[i].NewPath = p
		}
	}
}

func stripN(p string, n int) (string, bool) {
	for i := 0; i < n; i++ {
		j := strings.IndexByte(p, '/')
		if j < 0 {
			return p, false
		}
		p = p[j+1:]
	}
	return p, p != ""
}

func attachContext(f *match.Finding, lines []string) {
	lo, hi := f.Line-4, f.Line+3
	if lo < 0 {
		lo = 0
	}
	if hi > len(lines) {
		hi = len(lines)
	}
	if lo < hi {
		f.DocLines = lines[lo:hi]
		f.DocOffset = lo + 1
	}
}

// dedupe drops repeated findings for the same text on the same line (e.g. a
// flag found both as a code span and inside a command) and sorts the rest.
func dedupe(fs []match.Finding) []match.Finding {
	seen := map[string]bool{}
	out := fs[:0]
	for _, f := range fs {
		k := fmt.Sprintf("%s\x00%d\x00%s", f.File, f.Line, f.Anchor.Text)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Column < out[j].Column
	})
	return out
}
