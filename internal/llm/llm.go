// Package llm implements the optional second pass: each finding is sent to an
// OpenAI-compatible chat completions endpoint, which confirms or rejects it
// and may propose a corrected documentation line. It is never used unless
// explicitly enabled, and nothing it returns is applied automatically.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/vickhunter/helpdrift/internal/match"
)

// Environment variables read by FromEnv.
const (
	EnvBaseURL = "HELPDRIFT_LLM_BASE_URL" // e.g. https://api.openai.com/v1 or http://localhost:11434/v1
	EnvModel   = "HELPDRIFT_LLM_MODEL"
	EnvAPIKey  = "HELPDRIFT_LLM_API_KEY" // optional for local servers
)

// Client talks to one endpoint and model.
type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

// FromEnv builds a client from the HELPDRIFT_LLM_* environment variables.
func FromEnv() (*Client, error) {
	c := &Client{
		BaseURL: strings.TrimRight(os.Getenv(EnvBaseURL), "/"),
		Model:   os.Getenv(EnvModel),
		APIKey:  os.Getenv(EnvAPIKey),
		HTTP:    &http.Client{Timeout: 90 * time.Second},
	}
	if c.BaseURL == "" || c.Model == "" {
		return nil, fmt.Errorf("--llm needs %s and %s to be set", EnvBaseURL, EnvModel)
	}
	return c, nil
}

const systemPrompt = `You review documentation for accuracy after a code change.
You get: an excerpt of a documentation file, one phrase in it that an automatic check flagged, and the code lines before and after the change that caused the flag.
Decide whether the documentation is now wrong because of this code change.
Reply with exactly one JSON object and no other text:
{"verdict": "confirm" or "reject", "reason": "one short sentence", "replacement": "the corrected documentation line, or an empty string when rejecting"}
Only confirm when the excerpt describes something the new code no longer has or does. Keep any replacement minimal: change only what the code change requires.`

// Review asks the model about one finding. Errors are returned in the review
// (verdict "error") rather than failing the run.
func (c *Client) Review(ctx context.Context, f *match.Finding) *match.Review {
	content, err := c.complete(ctx, []message{{"system", systemPrompt}, {"user", prompt(f)}})
	if err != nil {
		return &match.Review{Verdict: "error", Reason: err.Error(), Model: c.Model}
	}
	r, err := parseReview(content)
	if err != nil {
		return &match.Review{Verdict: "error", Reason: err.Error(), Model: c.Model}
	}
	r.Model = c.Model
	return r
}

func prompt(f *match.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Documentation file: %s\n", f.File)
	fmt.Fprintf(&b, "Flagged phrase (line %d): %q (%s)\n\nExcerpt:\n", f.Line, f.Anchor.Text, f.Anchor.Kind)
	for i, l := range f.DocLines {
		fmt.Fprintf(&b, "%4d | %s\n", f.DocOffset+i, l)
	}
	fmt.Fprintf(&b, "\nAutomatic check result: %s\n", f.Change)
	if e := f.Code; e != nil {
		if e.OldLineText != "" {
			loc := e.OldFile
			line := e.OldLine
			if loc == "" {
				loc, line = e.File, e.Line
			}
			fmt.Fprintf(&b, "Code before (%s:%d):\n%s\n", loc, line, e.OldLineText)
		}
		if e.NewLineText != "" {
			fmt.Fprintf(&b, "Code after (%s:%d):\n%s\n", e.File, e.Line, e.NewLineText)
		} else if f.Change == match.Removed {
			b.WriteString("Code after: the line was removed and the phrase no longer appears anywhere in the code.\n")
		}
	}
	if f.Suggestion != "" {
		fmt.Fprintf(&b, "Mechanical suggestion: %s\n", f.Suggestion)
	}
	return b.String()
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *Client) complete(ctx context.Context, msgs []message) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": c.Model, "messages": msgs, "temperature": 0})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("llm endpoint returned %s: %s", resp.Status, truncate(string(raw), 200))
	}
	var out struct {
		Choices []struct {
			Message message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm endpoint: bad JSON: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("llm endpoint: no choices in response")
	}
	return out.Choices[0].Message.Content, nil
}

// parseReview extracts the JSON object from a model reply, tolerating code
// fences or text around it.
func parseReview(s string) (*match.Review, error) {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("llm reply is not JSON: %s", truncate(s, 120))
	}
	var r match.Review
	if err := json.Unmarshal([]byte(s[start:end+1]), &r); err != nil {
		return nil, fmt.Errorf("llm reply is not valid JSON: %w", err)
	}
	r.Verdict = strings.ToLower(strings.TrimSpace(r.Verdict))
	if r.Verdict != "confirm" && r.Verdict != "reject" {
		return nil, fmt.Errorf("llm reply has unknown verdict %q", r.Verdict)
	}
	return &r, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
