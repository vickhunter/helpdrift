package anchors

import (
	"fmt"
	"strings"
	"testing"
)

func render(as []Anchor) []string {
	var out []string
	for _, a := range as {
		out = append(out, fmt.Sprintf("%d:%d %s/%s %s", a.Line, a.Col, a.Kind, a.Source, a.Text))
	}
	return out
}

const doc = `---
title: Exporting invoices
---
# Export your invoices

Open [Billing](https://app.acme.test/settings/billing?tab=invoices) and click **Export CSV**.
Then choose "Download report" or press ` + "`Esc`" + `.

> **Note:** set ` + "`ACME_API_KEY`" + ` first, or pass ` + "`--token=abc`" + `.

Run ` + "`acme export --format csv --since 7d`" + ` to export from the terminal.
The **Pro** plan costs $12/month and includes a 14-day trial for up to 5 seats.

` + "```bash" + `
ACME_REGION=eu acme export --output report.csv
curl https://api.acme.test/v1/invoices/export
` + "```" + `

` + "```yaml" + `
export_format: csv
` + "```" + `

<!-- helpdrift-ignore-line --> Click **Old Button**.
Visit /settings/team or see the [guide](../guide.md).
`

func TestExtract(t *testing.T) {
	got := render(Extract([]byte(doc), Options{}))
	want := []string{
		"6:16 path/url /settings/billing",
		"6:81 label/bold Export CSV",
		"7:14 label/quote Download report",
		"7:41 code/code Esc",
		"9:18 env/code ACME_API_KEY",
		"9:48 flag/code --token",
		"11:6 code/code acme export --format csv --since 7d",
		"11:18 flag/code --format",
		"11:31 flag/code --since",
		"12:7 label/bold Pro",
		"12:24 price/prose $12",
		"12:49 quantity/prose 14-day",
		"12:72 quantity/prose 5 seats",
		"15:1 env/fence ACME_REGION",
		"15:28 flag/fence --output",
		"16:6 path/url /v1/invoices/export",
		"20:1 key/fence export_format",
		"24:7 path/prose /settings/team",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("anchors mismatch\n got:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestIgnoreAndFrontMatter(t *testing.T) {
	if as := Extract([]byte("---\nhelpdrift: ignore\n---\nClick **Export CSV**.\n"), Options{}); len(as) != 0 {
		t.Errorf("front matter ignore: got %v", render(as))
	}
	in := "<!-- helpdrift-ignore-start -->\n**Export CSV**\n<!-- helpdrift-ignore-end -->\n**Save changes**\n"
	got := render(Extract([]byte(in), Options{Ignore: []string{"save CHANGES"}}))
	if len(got) != 0 {
		t.Errorf("ignore block/list: got %v", got)
	}
}

func TestStopWordsAndMinLength(t *testing.T) {
	got := render(Extract([]byte("**Note**: press **OK** then **Save**.\n"), Options{}))
	want := []string{"1:31 label/bold Save"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v want %v", got, want)
	}
}
