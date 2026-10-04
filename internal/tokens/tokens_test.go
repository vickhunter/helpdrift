package tokens

import (
	"fmt"
	"strings"
	"testing"
)

func render(ts []Token) string {
	var parts []string
	for _, t := range ts {
		parts = append(parts, fmt.Sprintf("%s:%s", t.Kind, t.Text))
	}
	return strings.Join(parts, " ")
}

func TestLine(t *testing.T) {
	cases := []struct {
		file, line, want string
	}{
		{"main.go", `	out := flag.String("output", "report.csv", "where to write") // the --legacy flag`,
			`ident:out ident:flag ident:String string:output string:report.csv string:where to write`},
		{"config.go", `	key := os.Getenv("ACME_API_KEY")`,
			`ident:key ident:os ident:Getenv string:ACME_API_KEY`},
		{"Billing.tsx", `      <Button onClick={exportCsv}>Export CSV</Button>`,
			`ident:Button ident:onClick ident:exportCsv text:Export CSV ident:Button`},
		{"Billing.tsx", `        Archive invoices`, `text:Archive invoices`},
		{"Billing.tsx", `        return (`, `ident:return`},
		{"routes.ts", `  { path: "/settings/billing", page: "https://app.acme.test/settings/team?x=1" },`,
			`ident:path string:/settings/billing ident:page string:https://app.acme.test/settings/team?x=1 path:/settings/team`},
		{"plans.ts", `  { name: "Pro", priceMonthly: 12, label: "$12/month" },`,
			`ident:name string:Pro ident:priceMonthly number:12 ident:label string:$12/month price:$12`},
		{"run.sh", `acme export --format csv # --old`, `ident:acme ident:export flag:--format ident:csv`},
		{"config.yml", `export-format: csv  # default`, `ident:export-format ident:csv`},
		{"usage.py", `parser.add_argument("--dry-run", help="Set ACME_MODE=fast")`,
			`ident:parser ident:add_argument string:--dry-run ident:help string:Set ACME_MODE=fast env:ACME_MODE`},
	}
	for _, c := range cases {
		got := render(Line(c.line, OptionsFor(c.file)))
		if got != c.want {
			t.Errorf("%s: %q\n got: %s\nwant: %s", c.file, c.line, got, c.want)
		}
	}
}

func TestCommentLinesSkipped(t *testing.T) {
	for _, l := range []string{`// "Export CSV" was here`, `  * "Export CSV"`, `/* "x" */`} {
		if ts := Line(l, OptionsFor("a.ts")); len(ts) != 0 {
			t.Errorf("%q: expected no tokens, got %s", l, render(ts))
		}
	}
}
