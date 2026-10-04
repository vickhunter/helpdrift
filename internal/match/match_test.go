package match

import (
	"strings"
	"testing"

	"github.com/vickhunter/helpdrift/internal/anchors"
	"github.com/vickhunter/helpdrift/internal/diff"
	"github.com/vickhunter/helpdrift/internal/index"
)

func build(t *testing.T, d string) *ChangeSet {
	t.Helper()
	files, err := diff.Parse(strings.NewReader(d))
	if err != nil {
		t.Fatal(err)
	}
	return Build(files, func(string) bool { return true })
}

func TestRenamePairsLinesBySimilarity(t *testing.T) {
	cs := build(t, `--- a/en.json
+++ b/en.json
@@ -2,3 +2,2 @@
-  "billing.export": "Export CSV",
-  "billing.legacy": "Legacy export",
-  "billing.archive": "Archive invoices",
+  "billing.export": "Download report",
+  "billing.archive": "Archive all invoices",
`)
	if r := cs.Renames["Export CSV"]; len(r) != 1 || r[0].New != "Download report" || r[0].NewLine != 2 {
		t.Errorf("Export CSV renames: %+v", r)
	}
	if r := cs.Renames["Archive invoices"]; len(r) != 1 || r[0].New != "Archive all invoices" || r[0].NewLine != 3 {
		t.Errorf("Archive renames: %+v", r)
	}
	if r := cs.Renames["Legacy export"]; len(r) != 0 {
		t.Errorf("Legacy export should be a plain removal, got %+v", r)
	}
	if len(cs.Removed["Legacy export"]) != 1 {
		t.Errorf("Legacy export not recorded as removed")
	}
}

func TestMatchValueChangeInCents(t *testing.T) {
	cs := build(t, `--- a/plans.py
+++ b/plans.py
@@ -1 +1 @@
-PRO = Plan(name="Pro", price_cents=1200)
+PRO = Plan(name="Pro", price_cents=1500)
`)
	pres := index.NewPresence(nil, nil)
	f, ok := cs.Match(anchors.Anchor{Text: "$12.00", Kind: anchors.Price, Line: 1}, pres)
	if !ok || f.Change != Changed || f.Replace.To != "$15.00" || f.Code.Key != "price_cents" {
		t.Fatalf("got ok=%v %+v %+v", ok, f, f.Replace)
	}
	// A quantity whose unit does not fit the key is not matched.
	if _, ok := cs.Match(anchors.Anchor{Text: "1200 days", Kind: anchors.Quantity}, pres); ok {
		t.Error("unexpected quantity match")
	}
}

func TestStillPresentIsNotReported(t *testing.T) {
	cs := build(t, `--- a/a.ts
+++ b/a.ts
@@ -1 +1 @@
-const label = "Save changes";
+const other = 1;
`)
	a := anchors.Anchor{Text: "save Changes", Kind: anchors.Label}
	exact, fold := PresenceCandidates([]anchors.Anchor{a})
	pres := index.NewPresence(exact, fold)
	if _, ok := cs.Match(a, pres); !ok {
		t.Fatal("expected a case-insensitive removal finding")
	}
	pres.AddFile("b.tsx", []byte(`<Button>Save changes</Button>`))
	if f, ok := cs.Match(a, pres); ok {
		t.Fatalf("label still exists elsewhere, got %+v", f)
	}
}
