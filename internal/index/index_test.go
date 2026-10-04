package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vickhunter/helpdrift/internal/tokens"
)

func TestPresenceKinds(t *testing.T) {
	p := NewPresence([]string{"output", "Export CSV"}, []string{"export csv"})
	p.AddFile("main.go", []byte("package main\n\nvar output = 1 // \"Export CSV\" is a comment\n"))
	if !p.Has("output", Kinds(tokens.Ident), false) {
		t.Error("output should be present as an identifier")
	}
	if p.Has("output", Kinds(tokens.String, tokens.Flag), false) {
		t.Error("output is not present as a string or flag")
	}
	if p.Has("Export CSV", Kinds(tokens.String, tokens.Text), true) {
		t.Error("text in comments must not count")
	}
	p.AddFile("Page.tsx", []byte("<b>EXPORT CSV</b>\n"))
	if !p.Has("Export CSV", Kinds(tokens.Text), true) || p.Has("Export CSV", Kinds(tokens.Text), false) {
		t.Error("case-insensitive lookup mismatch")
	}
}

func TestFSTreeWalkSkipsBinaryAndVendor(t *testing.T) {
	dir := t.TempDir()
	write := func(p, s string) {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(s), 0o644)
	}
	write("src/a.ts", "x")
	write("node_modules/lib/b.js", "y")
	write("img.png", "\x89PNG\x00\x00")
	var got []string
	err := FSTree{Root: dir}.Walk(func(string) bool { return true }, func(p string, _ []byte) error {
		got = append(got, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "src/a.ts" {
		t.Errorf("walked %v", got)
	}
}
