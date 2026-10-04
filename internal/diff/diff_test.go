package diff

import (
	"strings"
	"testing"
)

const gitDiff = `diff --git a/cmd/acme/main.go b/cmd/acme/main.go
index 1111111..2222222 100644
--- a/cmd/acme/main.go
+++ b/cmd/acme/main.go
@@ -7 +7 @@ func main() {
-	output := flag.String("output", "report.csv", "where to write")
+	output := flag.String("out-file", "report.csv", "where to write")
@@ -12,2 +11,0 @@ func main() {
--- a removed line that looks like a header
-	legacy()
diff --git a/old.txt b/old.txt
deleted file mode 100644
index 3333333..0000000
--- a/old.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
diff --git a/new.txt b/new.txt
new file mode 100644
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
\ No newline at end of file
`

func TestParseGitDiff(t *testing.T) {
	files, err := Parse(strings.NewReader(gitDiff))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}
	f := files[0]
	if f.OldPath != "cmd/acme/main.go" || f.NewPath != "cmd/acme/main.go" {
		t.Errorf("paths = %q %q", f.OldPath, f.NewPath)
	}
	if len(f.Groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(f.Groups))
	}
	g := f.Groups[0]
	if len(g.Removed) != 1 || g.Removed[0].Num != 7 || !strings.Contains(g.Removed[0].Text, `"output"`) {
		t.Errorf("removed = %+v", g.Removed)
	}
	if len(g.Added) != 1 || g.Added[0].Num != 7 || !strings.Contains(g.Added[0].Text, `"out-file"`) {
		t.Errorf("added = %+v", g.Added)
	}
	g = f.Groups[1]
	if len(g.Removed) != 2 || g.Removed[0].Num != 12 || g.Removed[0].Text != "-- a removed line that looks like a header" {
		t.Errorf("second group removed = %+v", g.Removed)
	}
	if files[1].NewPath != "" || files[1].OldPath != "old.txt" {
		t.Errorf("deleted file paths = %q %q", files[1].OldPath, files[1].NewPath)
	}
	if files[2].OldPath != "" || files[2].NewPath != "new.txt" || len(files[2].Groups[0].Added) != 2 {
		t.Errorf("new file = %+v", files[2])
	}
}

func TestParsePlainDiffWithContext(t *testing.T) {
	in := `--- a/src/routes.ts	2026-10-01 10:00:00
+++ b/src/routes.ts	2026-10-02 10:00:00
@@ -1,4 +1,4 @@
 export const routes = [
-  { path: "/settings/billing" },
+  { path: "/account/billing" },
   { path: "/settings/team" },
-];
+]
--- a/b.txt
+++ b/b.txt
@@ -1 +1 @@
-x
+y
`
	files, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if files[0].NewPath != "src/routes.ts" {
		t.Errorf("path = %q", files[0].NewPath)
	}
	if len(files[0].Groups) != 2 {
		t.Fatalf("got %d groups, want 2 (context splits groups)", len(files[0].Groups))
	}
	if files[0].Groups[1].Removed[0].Num != 4 || files[0].Groups[1].Added[0].Num != 4 {
		t.Errorf("line numbers wrong: %+v", files[0].Groups[1])
	}
}
