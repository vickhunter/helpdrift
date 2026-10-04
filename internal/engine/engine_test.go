package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vickhunter/helpdrift/internal/anchors"
	"github.com/vickhunter/helpdrift/internal/match"
)

// fixtureRepo builds a throwaway git repository from
// testdata/fixtures/<name>/before (first commit) and .../after (second
// commit) and returns its path.
func fixtureRepo(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := filepath.Join("..", "..", "testdata", "fixtures", name)
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=helpdrift-test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	copyTree(t, filepath.Join(src, "before"), dir)
	git("add", "-A")
	git("commit", "-q", "-m", "before")
	clearTree(t, dir)
	copyTree(t, filepath.Join(src, "after"), dir)
	git("add", "-A")
	git("commit", "-q", "-m", "after")
	return dir
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func clearTree(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// lineOf returns the 1-based line of the first occurrence of substr.
func lineOf(t *testing.T, file, substr string) int {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, substr) {
			return i + 1
		}
	}
	t.Fatalf("%q not found in %s", substr, file)
	return 0
}

type want struct {
	doc, anchor, change, codeFile, to string
}

func checkFindings(t *testing.T, dir string, got []match.Finding, wants []want) {
	t.Helper()
	if len(got) != len(wants) {
		for _, f := range got {
			t.Logf("got: %s:%d %q %s %+v", f.File, f.Line, f.Anchor.Text, f.Change, f.Code)
		}
		t.Fatalf("got %d findings, want %d", len(got), len(wants))
	}
	for i, w := range wants {
		f := got[i]
		line := lineOf(t, filepath.Join(dir, w.doc), w.anchor)
		if f.File != w.doc || f.Line != line || f.Anchor.Text != w.anchor || f.Change != w.change {
			t.Errorf("finding %d: got %s:%d %q %s, want %s:%d %q %s", i, f.File, f.Line, f.Anchor.Text, f.Change, w.doc, line, w.anchor, w.change)
		}
		if f.Code == nil || f.Code.File != w.codeFile || f.Code.Line <= 0 {
			t.Errorf("finding %d: code evidence %+v, want file %s", i, f.Code, w.codeFile)
		}
		if w.to != "" && (f.Replace == nil || f.Replace.To != w.to) {
			t.Errorf("finding %d: replacement %+v, want %q", i, f.Replace, w.to)
		}
		if w.to == "" && f.Replace != nil {
			t.Errorf("finding %d: unexpected replacement %+v", i, f.Replace)
		}
	}
}

func TestFixtures(t *testing.T) {
	cases := []struct {
		name  string
		wants []want
	}{
		{"renamed-flag", []want{
			{"docs/cli.md", "--output", match.Renamed, "cmd/acme/main.go", "--out-file"},
		}},
		{"removed-env", []want{
			{"docs/configuration.md", "ACME_LEGACY_TOKEN", match.Removed, "internal/config/config.go", ""},
		}},
		{"changed-path", []want{
			{"help/billing.md", "/settings/billing", match.Renamed, "src/routes.ts", "/account/billing"},
		}},
		{"renamed-label", []want{
			{"help/export.md", "Export CSV", match.Renamed, "src/ui/BillingPage.tsx", "Download report"},
			{"help/export.md", "Archive invoices", match.Renamed, "src/ui/BillingPage.tsx", "Archive all invoices"},
		}},
		{"changed-price", []want{
			{"help/pricing.md", "$12", match.Changed, "src/plans.ts", "$15"},
		}},
		{"non-matches", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fixtureRepo(t, c.name)
			// Committed range (CI style) and working tree against a base ref.
			for _, head := range []string{"HEAD", ""} {
				res, err := Check(Options{Root: dir, Base: "HEAD~1", Head: head})
				if err != nil {
					t.Fatal(err)
				}
				checkFindings(t, dir, res.Findings, c.wants)
			}
		})
	}
}

func TestDemoFixture(t *testing.T) {
	dir := fixtureRepo(t, "demo")
	res, err := Check(Options{Root: dir, Base: "HEAD~1", Head: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, dir, res.Findings, []want{
		{"help/billing/export.md", "/settings/billing", match.Renamed, "web/src/routes.ts", "/account/billing"},
		{"help/billing/export.md", "Export CSV", match.Renamed, "web/src/ui/BillingPage.tsx", "Download report"},
		{"help/billing/export.md", "Archive invoices", match.Renamed, "web/src/ui/BillingPage.tsx", "Archive all invoices"},
		{"help/billing/plans.mdx", "$12", match.Changed, "web/src/plans.ts", "$15"},
		{"help/cli/export.md", "--output", match.Renamed, "cmd/acme/main.go", "--out-file"},
		{"help/cli/export.md", "ACME_LEGACY_TOKEN", match.Removed, "internal/config/config.go", ""},
	})
	if res.Stats.ChangedFiles != 5 {
		t.Errorf("changed code files = %d, want 5 (CHANGELOG is not code)", res.Stats.ChangedFiles)
	}
}

func TestDocsGlobAndIgnore(t *testing.T) {
	dir := fixtureRepo(t, "demo")
	res, err := Check(Options{Root: dir, Base: "HEAD~1", Head: "HEAD", Docs: []string{"help/cli/**"}, Ignore: []string{"acme_legacy_token"}})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, dir, res.Findings, []want{
		{"help/cli/export.md", "--output", match.Renamed, "cmd/acme/main.go", "--out-file"},
	})
}

func TestDiffFileMode(t *testing.T) {
	// A diff file plus a plain directory (no git) holding the new tree.
	root := filepath.Join("..", "..", "testdata", "fixtures", "renamed-flag", "after")
	res, err := Check(Options{Root: root, DiffFile: filepath.Join("..", "..", "testdata", "renamed-flag.diff"), Strip: -1})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, root, res.Findings, []want{
		{"docs/cli.md", "--output", match.Renamed, "cmd/acme/main.go", "--out-file"},
	})
}

func TestScan(t *testing.T) {
	dir := fixtureRepo(t, "demo")
	res, err := Scan(Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range res.Findings {
		got = append(got, f.File+" "+f.Anchor.Text+" "+f.Change)
	}
	want := []string{
		"help/cli/export.md --output missing",
		"help/cli/export.md ACME_LEGACY_TOKEN missing",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("scan findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	res, err = Scan(Options{Root: dir, Kinds: []anchors.Kind{anchors.Path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 || res.Findings[0].Anchor.Text != "/settings/billing" {
		t.Errorf("path scan: %+v", res.Findings)
	}
}
