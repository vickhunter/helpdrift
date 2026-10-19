package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	fixtureRoot = filepath.Join("..", "..", "testdata", "fixtures", "renamed-flag", "after")
	fixtureDiff = filepath.Join("..", "..", "testdata", "renamed-flag.diff")
)

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func TestCheckTextAndExitCodes(t *testing.T) {
	code, out, stderr := runCLI(t, "check", "--root", fixtureRoot, "--diff", fixtureDiff)
	if code != exitOK {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	for _, want := range []string{
		`docs/cli.md:3  "--output"  [flag]`,
		`renamed in cmd/acme/main.go:9  "output" -> "out-file"`,
		`suggestion: replace "--output" with "--out-file"`,
		"helpdrift check: 1 finding in 1 doc",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if code, _, _ := runCLI(t, "check", "--root", fixtureRoot, "--diff", fixtureDiff, "--fail-on-findings"); code != exitFindings {
		t.Errorf("--fail-on-findings: exit %d, want %d", code, exitFindings)
	}
	if code, _, _ := runCLI(t, "check", "--root", fixtureRoot, "--diff", fixtureDiff, "--fail-on-findings", "--ignore", "--output"); code != exitOK {
		t.Errorf("ignored anchor: exit %d, want 0", code)
	}
}

func TestCheckJSONAndSideReports(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "r.md")
	code, out, stderr := runCLI(t, "check", "--root", fixtureRoot, "--diff", fixtureDiff, "--format", "json", "--markdown-file", md)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var res struct {
		Mode     string `json:"mode"`
		Findings []struct {
			File   string `json:"file"`
			Line   int    `json:"line"`
			Change string `json:"change"`
			Anchor struct {
				Text string `json:"text"`
				Kind string `json:"kind"`
			} `json:"anchor"`
			Code struct {
				File string `json:"file"`
				Line int    `json:"line"`
				Old  string `json:"old"`
				New  string `json:"new"`
			} `json:"code"`
			Replace struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"replace"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if res.Mode != "check" || len(res.Findings) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	f := res.Findings[0]
	if f.File != "docs/cli.md" || f.Line != 3 || f.Change != "renamed" || f.Anchor.Kind != "flag" ||
		f.Code.File != "cmd/acme/main.go" || f.Code.Line != 9 || f.Replace.To != "--out-file" {
		t.Errorf("unexpected finding: %+v", f)
	}
	b, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "<!-- helpdrift-report -->") || !strings.Contains(string(b), "| `docs/cli.md:3` | `--output` | renamed → `--out-file`") {
		t.Errorf("markdown report:\n%s", b)
	}
}

func TestUsageErrors(t *testing.T) {
	if code, _, _ := runCLI(t); code != exitError {
		t.Errorf("no args: exit %d", code)
	}
	if code, _, _ := runCLI(t, "frobnicate"); code != exitError {
		t.Errorf("unknown command: exit %d", code)
	}
	t.Setenv("GITHUB_BASE_REF", "")
	if code, _, stderr := runCLI(t, "check", "--root", t.TempDir()); code != exitError || !strings.Contains(stderr, "--base") {
		t.Errorf("missing base: exit %d, stderr %q", code, stderr)
	}
	if code, _, _ := runCLI(t, "check", "--root", fixtureRoot, "--diff", fixtureDiff, "--format", "xml"); code != exitError {
		t.Errorf("bad format: exit %d", code)
	}
}

func TestDocsQuoteFormatExample(t *testing.T) {
	root := filepath.Join("..", "..", "docs")
	for _, name := range []string{"check.md", "formats.md"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), formatMDExample) {
			t.Errorf("%s does not quote %q", name, formatMDExample)
		}
	}
}

func TestLLMNeedsEnv(t *testing.T) {
	t.Setenv("HELPDRIFT_LLM_BASE_URL", "")
	t.Setenv("HELPDRIFT_LLM_MODEL", "")
	code, _, stderr := runCLI(t, "check", "--root", fixtureRoot, "--diff", fixtureDiff, "--llm")
	if code != exitError || !strings.Contains(stderr, "HELPDRIFT_LLM_BASE_URL") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
