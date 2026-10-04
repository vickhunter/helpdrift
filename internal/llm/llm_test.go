package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vickhunter/helpdrift/internal/anchors"
	"github.com/vickhunter/helpdrift/internal/match"
)

func TestReviewAgainstFakeServer(t *testing.T) {
	var gotAuth, gotModel, gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		var req struct {
			Model    string    `json:"model"`
			Messages []message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		gotPrompt = req.Messages[1].Content
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` +
			"```json\\n{\\\"verdict\\\": \\\"Confirm\\\", \\\"reason\\\": \\\"The button was renamed.\\\", \\\"replacement\\\": \\\"Click **Download report**.\\\"}\\n```" +
			`"}}]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1", Model: "test-model", APIKey: "k", HTTP: srv.Client()}
	f := &match.Finding{File: "help/billing.md", Line: 3, Change: match.Renamed,
		Anchor:   anchors.Anchor{Text: "Export CSV", Kind: anchors.Label},
		Code:     &match.Evidence{File: "src/Billing.tsx", Line: 9, Old: "Export CSV", New: "Download report", OldLineText: "<b>Export CSV</b>", NewLineText: "<b>Download report</b>"},
		DocLines: []string{"# Billing", "", "Click **Export CSV**."}, DocOffset: 1}
	r := c.Review(context.Background(), f)
	if r.Verdict != "confirm" || r.Replacement != "Click **Download report**." || r.Model != "test-model" {
		t.Fatalf("unexpected review: %+v", r)
	}
	if gotAuth != "Bearer k" || gotModel != "test-model" {
		t.Errorf("auth=%q model=%q", gotAuth, gotModel)
	}
	for _, want := range []string{`"Export CSV"`, "Code after (src/Billing.tsx:9)", "   3 | Click **Export CSV**."} {
		if !strings.Contains(gotPrompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, gotPrompt)
		}
	}
}

func TestReviewErrorIsNotFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	r := c.Review(context.Background(), &match.Finding{})
	if r.Verdict != "error" || !strings.Contains(r.Reason, "401") {
		t.Fatalf("got %+v", r)
	}
}

func TestFromEnvRequiresSettings(t *testing.T) {
	t.Setenv(EnvBaseURL, "")
	t.Setenv(EnvModel, "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error without env")
	}
}
