package config

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	in := `# helpdrift settings
docs: ["help/**/*.md", 'docs/**/*.mdx']
code:
  - src/**
  - "locales/**"   # translations
exclude: src/generated/**
ignore: [Settings, "Dashboard"]
min_length: 4
`
	c, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Docs:      []string{"help/**/*.md", "docs/**/*.mdx"},
		Code:      []string{"src/**", "locales/**"},
		Exclude:   []string{"src/generated/**"},
		Ignore:    []string{"Settings", "Dashboard"},
		MinLength: 4,
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{"docz: [a]\n", "- a\n", "llm:\n  model: x\n", "min_length: zero\n", "docs: [a\n"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}
