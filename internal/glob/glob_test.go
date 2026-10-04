package glob

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.md", "README.md", true},
		{"**/*.md", "help/billing/export.md", true},
		{"*.md", "help/billing/export.md", true},
		{"help/**/*.md", "help/export.md", true},
		{"help/**/*.md", "help/a/b/export.md", true},
		{"help/**/*.md", "docs/export.md", false},
		{"docs/*.mdx", "docs/a/b.mdx", false},
		{"**/*.{md,mdx}", "docs/a/b.mdx", true},
		{"{help,docs}/**", "docs/x.go", true},
		{"node_modules", "web/node_modules/react/index.js", true},
		{"src/", "src/ui/App.tsx", true},
		{"src/**", "lib/ui/App.tsx", false},
		{"CHANGELOG*", "CHANGELOG.md", true},
		{"CHANGELOG*", "pkg/CHANGELOG.md", true},
		{"./src/*.ts", "src/a.ts", true},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.name); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
