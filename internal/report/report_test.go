package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vickhunter/helpdrift/internal/anchors"
	"github.com/vickhunter/helpdrift/internal/engine"
	"github.com/vickhunter/helpdrift/internal/match"
)

func TestMarkdownEscapesAndSummary(t *testing.T) {
	res := &engine.Result{Mode: "check", Stats: engine.Stats{Docs: 1, Anchors: 1, ChangedFiles: 1}, Findings: []match.Finding{{
		File: "help/a.md", Line: 2, Anchor: anchors.Anchor{Text: "A | B", Kind: anchors.Label}, Change: match.Renamed,
		Code:       &match.Evidence{File: "src/a.ts", Line: 9, Old: "A | B", New: "C`D"},
		Replace:    &match.Replacement{From: "A | B", To: "C`D"},
		Suggestion: `replace "A | B" with "C` + "`" + `D"`,
	}}}
	var b bytes.Buffer
	if err := Markdown(&b, res); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{Marker, "| `help/a.md:2` | `A \\| B` | renamed → `C'D` | `src/a.ts:9` |",
		"1 finding in 1 doc (1 doc, 1 anchor checked against 1 changed code file)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	b.Reset()
	if err := Write(&b, "markdown", &engine.Result{Mode: "check"}); err != nil || !strings.Contains(b.String(), "no doc claims affected") {
		t.Errorf("empty report: %v %q", err, b.String())
	}
}
