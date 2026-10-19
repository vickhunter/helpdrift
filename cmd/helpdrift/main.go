// Command helpdrift flags Markdown/MDX docs that mention things a code
// change removed or renamed.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vickhunter/helpdrift/internal/anchors"
	"github.com/vickhunter/helpdrift/internal/config"
	"github.com/vickhunter/helpdrift/internal/engine"
	"github.com/vickhunter/helpdrift/internal/llm"
	"github.com/vickhunter/helpdrift/internal/report"
)

var version = "dev"

// formatMDExample is the Markdown report command the docs site quotes.
// scripts/demo-rename.sh rewrites this literal in Go source and leaves docs/ alone,
// which is the drift the GitLab demo merge request is meant to catch.
const formatMDExample = "helpdrift check --format md"

// Exit codes.
const (
	exitOK       = 0
	exitFindings = 1
	exitError    = 2
)

const usage = `helpdrift checks Markdown/MDX docs against code changes.

Usage:
  helpdrift check --base <ref> [--head <ref>] [flags]   docs vs. a git diff (PR mode)
  helpdrift check --diff <file|-> [flags]               docs vs. a unified diff file
  helpdrift scan [flags]                                docs vs. the current code
  helpdrift anchors <file.md>...                        show the claims found in docs
  helpdrift version

Run "helpdrift <command> -h" for the flags of a command.

Exit codes: 0 = ok, 1 = findings (with --fail-on-findings), 2 = error.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitError
	}
	switch args[0] {
	case "check", "scan":
		return runCheck(args[0], args[1:], stdin, stdout, stderr)
	case "anchors":
		return runAnchors(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "helpdrift", version)
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	fmt.Fprintf(stderr, "helpdrift: unknown command %q\n\n%s", args[0], usage)
	return exitError
}

// listFlag collects comma-separated and repeated values.
type listFlag struct {
	vals []string
	set  bool
}

func (l *listFlag) String() string { return strings.Join(l.vals, ",") }
func (l *listFlag) Set(s string) error {
	l.set = true
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			l.vals = append(l.vals, v)
		}
	}
	return nil
}

func runCheck(mode string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("helpdrift "+mode, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		docs, code, exclude, ignore, kinds listFlag
		root                               = fs.String("root", ".", "repository root")
		cfgPath                            = fs.String("config", "", "config file (default: .helpdrift.yml in --root, if present)")
		base, head, diffFile               string
		strip                              int
		format                             = fs.String("format", "text", "output format: text, json or markdown ("+formatMDExample+")")
		output                             = fs.String("output", "", "write the report to this file instead of stdout")
		mdFile                             = fs.String("markdown-file", "", "also write a Markdown report to this file")
		jsonFile                           = fs.String("json-file", "", "also write a JSON report to this file")
		minLen                             = fs.Int("min-length", 0, "minimum anchor length (default 3)")
		failOn                             = fs.Bool("fail-on-findings", false, "exit with status 1 when there are findings")
		useLLM                             = fs.Bool("llm", false, "review findings with an OpenAI-compatible model (needs "+llm.EnvBaseURL+" and "+llm.EnvModel+")")
		llmMax                             = fs.Int("llm-max", 20, "review at most this many findings with --llm")
	)
	fs.Var(&docs, "docs", "doc globs, comma-separated or repeated (default **/*.md,**/*.mdx)")
	fs.Var(&code, "code", "code globs; default: every file that is not a doc")
	fs.Var(&exclude, "exclude", "globs to exclude from docs and code")
	fs.Var(&ignore, "ignore", "anchors to never report (case-insensitive)")
	if mode == "check" {
		fs.StringVar(&base, "base", "", "git ref to compare against (default: origin/$GITHUB_BASE_REF when set)")
		fs.StringVar(&head, "head", "", "git ref with the new code (default: the working tree)")
		fs.StringVar(&diffFile, "diff", "", "read a unified diff from this file ('-' for stdin) instead of running git")
		fs.IntVar(&strip, "strip", -1, "with --diff: leading path components to strip, like patch -p (default: auto)")
	} else {
		fs.StringVar(&head, "head", "", "git ref to scan (default: the working tree)")
		fs.Var(&kinds, "kinds", "anchor kinds to audit: flag, env, path, code, key, label (default flag,env)")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitError
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "helpdrift %s: unexpected arguments: %s\n", mode, strings.Join(fs.Args(), " "))
		return exitError
	}

	// Config file first, then flags on top.
	var cfg config.Config
	p := *cfgPath
	if p == "" {
		p = config.Find(*root)
	}
	if p != "" {
		c, err := config.Load(p)
		if err != nil {
			fmt.Fprintln(stderr, "helpdrift:", err)
			return exitError
		}
		cfg = c
	}
	pick := func(f listFlag, fromCfg []string) []string {
		if f.set {
			return f.vals
		}
		return fromCfg
	}
	opts := engine.Options{
		Root:      *root,
		Base:      base,
		Head:      head,
		DiffFile:  diffFile,
		Strip:     strip,
		Stdin:     stdin,
		Docs:      pick(docs, cfg.Docs),
		Code:      pick(code, cfg.Code),
		Exclude:   pick(exclude, cfg.Exclude),
		Ignore:    append(append([]string{}, cfg.Ignore...), ignore.vals...),
		MinLength: cfg.MinLength,
	}
	if *minLen > 0 {
		opts.MinLength = *minLen
	}
	for _, k := range kinds.vals {
		opts.Kinds = append(opts.Kinds, anchors.Kind(k))
	}
	if mode == "check" && opts.Base == "" && opts.DiffFile == "" {
		if b := os.Getenv("GITHUB_BASE_REF"); b != "" {
			opts.Base = "origin/" + b
		}
	}

	var res *engine.Result
	var err error
	if mode == "check" {
		res, err = engine.Check(opts)
	} else {
		res, err = engine.Scan(opts)
	}
	if err != nil {
		fmt.Fprintf(stderr, "helpdrift %s: %v\n", mode, err)
		return exitError
	}

	if *useLLM && len(res.Findings) > 0 {
		client, err := llm.FromEnv()
		if err != nil {
			fmt.Fprintln(stderr, "helpdrift:", err)
			return exitError
		}
		for i := range res.Findings {
			if i >= *llmMax {
				fmt.Fprintf(stderr, "helpdrift: --llm-max reached, %d findings not reviewed\n", len(res.Findings)-i)
				break
			}
			res.Findings[i].Review = client.Review(context.Background(), &res.Findings[i])
		}
	}

	if err := writeReport(*output, *format, res, stdout); err != nil {
		fmt.Fprintln(stderr, "helpdrift:", err)
		return exitError
	}
	for _, side := range []struct{ path, format string }{{*mdFile, "markdown"}, {*jsonFile, "json"}} {
		if side.path == "" {
			continue
		}
		if err := writeReport(side.path, side.format, res, nil); err != nil {
			fmt.Fprintln(stderr, "helpdrift:", err)
			return exitError
		}
	}

	if *failOn && countActionable(res) > 0 {
		return exitFindings
	}
	return exitOK
}

// countActionable counts findings, leaving out those an LLM review rejected.
func countActionable(res *engine.Result) int {
	n := 0
	for _, f := range res.Findings {
		if f.Review == nil || f.Review.Verdict != "reject" {
			n++
		}
	}
	return n
}

func writeReport(path, format string, res *engine.Result, stdout io.Writer) error {
	var buf bytes.Buffer
	if err := report.Write(&buf, format, res); err != nil {
		return err
	}
	if path == "" || path == "-" {
		_, err := stdout.Write(buf.Bytes())
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func runAnchors(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("helpdrift anchors", flag.ContinueOnError)
	fs.SetOutput(stderr)
	minLen := fs.Int("min-length", 3, "minimum anchor length")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitError
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(stderr, "usage: helpdrift anchors <file.md>...")
		return exitError
	}
	for _, p := range fs.Args() {
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(stderr, "helpdrift:", err)
			return exitError
		}
		for _, a := range anchors.Extract(data, anchors.Options{MinLength: *minLen}) {
			fmt.Fprintf(stdout, "%s:%d:%d\t%s\t%s\t%s\n", p, a.Line, a.Col, a.Kind, a.Source, a.Text)
		}
	}
	return exitOK
}
