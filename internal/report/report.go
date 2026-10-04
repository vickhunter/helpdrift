// Package report renders results as plain text, JSON or Markdown.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/vickhunter/helpdrift/internal/engine"
	"github.com/vickhunter/helpdrift/internal/match"
)

// Formats lists the supported output formats.
var Formats = []string{"text", "json", "markdown"}

// Write renders res in the given format.
func Write(w io.Writer, format string, res *engine.Result) error {
	switch format {
	case "", "text":
		return Text(w, res)
	case "json":
		return JSON(w, res)
	case "markdown", "md":
		return Markdown(w, res)
	}
	return fmt.Errorf("unknown format %q (want one of %s)", format, strings.Join(Formats, ", "))
}

// JSON writes the result as indented JSON.
func JSON(w io.Writer, res *engine.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(res)
}

func quoteAnchor(f match.Finding) string {
	return strconv.Quote(f.Anchor.Text)
}

func codeLoc(e *match.Evidence) string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", e.File, e.Line)
}

// what describes the code change in one line.
func what(f match.Finding) string {
	e := f.Code
	switch f.Change {
	case match.Renamed:
		return fmt.Sprintf("renamed in %s  %s -> %s", codeLoc(e), strconv.Quote(e.Old), strconv.Quote(e.New))
	case match.Changed:
		return fmt.Sprintf("value changed in %s  %s: %s -> %s", codeLoc(e), e.Key, e.Old, e.New)
	case match.Removed:
		return fmt.Sprintf("removed from %s (line in base)  %s", codeLoc(e), strconv.Quote(e.Old))
	case match.Missing:
		return "not found anywhere in the code"
	}
	return f.Change
}

// Text writes a human-readable report.
func Text(w io.Writer, res *engine.Result) error {
	for _, f := range res.Findings {
		fmt.Fprintf(w, "%s:%d  %s  [%s]\n", f.File, f.Line, quoteAnchor(f), f.Anchor.Kind)
		fmt.Fprintf(w, "  %s\n", what(f))
		if f.Suggestion != "" {
			fmt.Fprintf(w, "  suggestion: %s\n", f.Suggestion)
		}
		if r := f.Review; r != nil {
			fmt.Fprintf(w, "  llm: %s", r.Verdict)
			if r.Reason != "" {
				fmt.Fprintf(w, " - %s", r.Reason)
			}
			fmt.Fprintln(w)
			if r.Replacement != "" {
				fmt.Fprintf(w, "  llm suggested line: %s\n", r.Replacement)
			}
		}
		fmt.Fprintln(w)
	}
	_, err := fmt.Fprintln(w, Summary(res))
	return err
}

// Summary is a one-line description of the result.
func Summary(res *engine.Result) string {
	files := map[string]bool{}
	for _, f := range res.Findings {
		files[f.File] = true
	}
	s := res.Stats
	head := fmt.Sprintf("%d %s in %d %s", len(res.Findings), plural(len(res.Findings), "finding", "findings"),
		len(files), plural(len(files), "doc", "docs"))
	if res.Mode == "scan" {
		return fmt.Sprintf("helpdrift scan: %s (%d %s, %d %s, %d code %s)", head,
			s.Docs, plural(s.Docs, "doc", "docs"), s.Anchors, plural(s.Anchors, "anchor", "anchors"),
			s.CodeFiles, plural(s.CodeFiles, "file", "files"))
	}
	return fmt.Sprintf("helpdrift check: %s (%d %s, %d %s checked against %d changed code %s)", head,
		s.Docs, plural(s.Docs, "doc", "docs"), s.Anchors, plural(s.Anchors, "anchor", "anchors"),
		s.ChangedFiles, plural(s.ChangedFiles, "file", "files"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Marker is the first line of the Markdown report, so a CI job can find and
// update its previous comment.
const Marker = "<!-- helpdrift-report -->"

// Markdown writes a report suitable for a pull request comment.
func Markdown(w io.Writer, res *engine.Result) error {
	fmt.Fprintln(w, Marker)
	if len(res.Findings) == 0 {
		fmt.Fprintln(w, "### helpdrift: no doc claims affected")
		fmt.Fprintln(w)
		fmt.Fprintln(w, Summary(res))
		return nil
	}
	fmt.Fprintf(w, "### helpdrift: %d doc %s may be out of date\n\n", len(res.Findings), plural(len(res.Findings), "claim", "claims"))
	fmt.Fprintln(w, "| Doc | Claim | Change | Code | Suggestion |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	for _, f := range res.Findings {
		change := f.Change
		if f.Replace != nil {
			change += " → " + mdCode(f.Replace.To)
		}
		sugg := f.Suggestion
		if f.Review != nil {
			sugg += " (llm: " + f.Review.Verdict + ")"
		}
		code := ""
		if f.Code != nil {
			code = mdCode(codeLoc(f.Code))
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n",
			mdCode(fmt.Sprintf("%s:%d", f.File, f.Line)), mdCode(f.Anchor.Text), change, code, mdCell(sugg))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "<sub>"+mdCell(Summary(res))+"</sub>")
	return nil
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}

func mdCode(s string) string {
	s = strings.ReplaceAll(mdCell(s), "`", "'")
	return "`" + s + "`"
}
