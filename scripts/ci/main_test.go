package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vickhunter/helpdrift/internal/report"
)

func TestUpsertNoteUpdatesExisting(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Header.Get("JOB-TOKEN") != "job-token" {
			t.Errorf("JOB-TOKEN = %q", r.Header.Get("JOB-TOKEN"))
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]noteJSON{{ID: 9, Body: report.Marker + "\nold"}})
			return
		}
		if r.Method != http.MethodPut || !strings.HasSuffix(r.URL.Path, "/notes/9") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["body"] != "new report" {
			t.Errorf("body = %q", body["body"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code := testEnv(srv).upsertNote("new report")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if len(methods) != 2 || !strings.HasPrefix(methods[1], "PUT ") {
		t.Fatalf("methods = %v", methods)
	}
}

func TestUpsertNoteCreatesWhenMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]noteJSON{})
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	if code := testEnv(srv).upsertNote("report"); code != exitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestUpsertNoteForbiddenDoesNotFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	stderr := captureStderr(t, func() {
		if code := testEnv(srv).upsertNote("report"); code != exitOK {
			t.Errorf("exit %d, want 0", code)
		}
	})
	if strings.Contains(stderr, "job-token") {
		t.Fatalf("stderr leaked the token: %s", stderr)
	}
	if !strings.Contains(stderr, "cannot write merge request notes") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestCreateIssue(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/42/issues" {
			t.Errorf("path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	if code := testEnv(srv).createIssue("scan report"); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if got["title"] != "docs: helpdrift scan found drift" || got["labels"] != "docs-drift" || got["description"] != "scan report" {
		t.Fatalf("payload = %#v", got)
	}
}

func TestCreateIssueForbiddenDoesNotFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	if code := testEnv(srv).createIssue("scan report"); code != exitOK {
		t.Fatalf("exit %d", code)
	}
}

func testEnv(srv *httptest.Server) apiEnv {
	return apiEnv{API: srv.URL, Project: "42", Token: "job-token", MRIID: "7", Client: srv.Client()}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
