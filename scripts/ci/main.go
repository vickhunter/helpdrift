// Command ci posts helpdrift reports to the GitLab API with CI_JOB_TOKEN.
// It stores no tokens. The job token is a predefined CI variable.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/vickhunter/helpdrift/internal/report"
)

const usage = `Usage:
  ci note <markdown-file>    create or update the merge request note
  ci issue <markdown-file>   open an issue when a scheduled scan finds drift

Environment: CI_API_V4_URL, CI_PROJECT_ID, CI_JOB_TOKEN.
note also needs CI_MERGE_REQUEST_IID.
`

const (
	noteDocs = "https://docs.gitlab.com/ci/jobs/ci_job_token/ and " +
		"https://docs.gitlab.com/ci/jobs/fine_grained_permissions/ and " +
		"https://docs.gitlab.com/api/notes/"
	issueDocs = "https://docs.gitlab.com/ci/jobs/fine_grained_permissions/"
)

type apiEnv struct {
	API     string
	Project string
	Token   string
	MRIID   string
	Client  *http.Client
}

type noteJSON struct {
	ID   int    `json:"id"`
	Body string `json:"body"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "note":
		os.Exit(cmdNote(os.Args[2:]))
	case "issue":
		os.Exit(cmdIssue(os.Args[2:]))
	case "help", "-h", "--help":
		fmt.Fprint(os.Stdout, usage)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func cmdNote(args []string) int {
	if len(args) != 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	body, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	env := fromEnv()
	if env.API == "" || env.Project == "" || env.Token == "" || env.MRIID == "" {
		fmt.Fprintln(os.Stderr, "note: CI_API_V4_URL, CI_PROJECT_ID, CI_JOB_TOKEN and CI_MERGE_REQUEST_IID are required")
		return 2
	}
	return env.upsertNote(string(body))
}

func cmdIssue(args []string) int {
	if len(args) != 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	body, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	env := fromEnv()
	if env.API == "" || env.Project == "" || env.Token == "" {
		fmt.Fprintln(os.Stderr, "issue: CI_API_V4_URL, CI_PROJECT_ID and CI_JOB_TOKEN are required")
		return 2
	}
	return env.createIssue(string(body))
}

func fromEnv() apiEnv {
	return apiEnv{
		API:     strings.TrimRight(os.Getenv("CI_API_V4_URL"), "/"),
		Project: os.Getenv("CI_PROJECT_ID"),
		Token:   os.Getenv("CI_JOB_TOKEN"),
		MRIID:   os.Getenv("CI_MERGE_REQUEST_IID"),
		Client:  http.DefaultClient,
	}
}

func (e apiEnv) client() *http.Client {
	if e.Client != nil {
		return e.Client
	}
	return http.DefaultClient
}

func (e apiEnv) endpoint(path string) string {
	return e.API + "/projects/" + url.PathEscape(e.Project) + path
}

func (e apiEnv) notesPath() string {
	return "/merge_requests/" + url.PathEscape(e.MRIID) + "/notes"
}

// Process exit codes for one API attempt. exitForbidden is internal:
// callers turn a final refusal into exit 0 so a read-only job token does
// not hide a green drift check. The Markdown file stays a job artifact.
const (
	exitOK        = 0
	exitFail      = 1
	exitForbidden = 3
)

// upsertNote updates the existing helpdrift note when the job token can write,
// and otherwise creates one.
func (e apiEnv) upsertNote(body string) int {
	status, raw, err := e.request(http.MethodGet, e.notesPath()+"?per_page=100", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "note: list:", err)
		return exitFail
	}
	if status >= 200 && status < 300 {
		var notes []noteJSON
		if err := json.Unmarshal(raw, &notes); err != nil {
			fmt.Fprintln(os.Stderr, "note: list:", err)
			return exitFail
		}
		for _, n := range notes {
			if !strings.Contains(n.Body, report.Marker) {
				continue
			}
			code := e.write(http.MethodPut, fmt.Sprintf("%s/%d", e.notesPath(), n.ID), body, "updated note")
			if code == exitForbidden {
				break
			}
			return code
		}
	}
	code := e.write(http.MethodPost, e.notesPath(), body, "created note")
	if code == exitForbidden {
		fmt.Fprintf(os.Stderr, "note: CI_JOB_TOKEN cannot write merge request notes (%s). The report is a job artifact.\n", noteDocs)
		return exitOK
	}
	return code
}

func (e apiEnv) createIssue(body string) int {
	payload := map[string]string{
		"title":       "docs: helpdrift scan found drift",
		"description": body,
		"labels":      "docs-drift",
	}
	status, raw, err := e.request(http.MethodPost, "/issues", payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, "issue:", err)
		return exitFail
	}
	if status >= 200 && status < 300 {
		fmt.Fprintf(os.Stderr, "issue: opened (%d)\n", status)
		return exitOK
	}
	if forbiddenStatus(status) {
		fmt.Fprintf(os.Stderr, "issue: HTTP %d. CI_JOB_TOKEN cannot create issues (%s). The scan report is a job artifact.\n%s\n", status, issueDocs, clip(raw))
		return exitOK
	}
	fmt.Fprintf(os.Stderr, "issue: HTTP %d\n%s\n", status, clip(raw))
	return exitFail
}

func (e apiEnv) write(method, path, body, okMsg string) int {
	status, raw, err := e.request(method, path, map[string]string{"body": body})
	if err != nil {
		fmt.Fprintln(os.Stderr, "note:", err)
		return exitFail
	}
	if status >= 200 && status < 300 {
		fmt.Fprintf(os.Stderr, "note: %s (%d)\n", okMsg, status)
		return exitOK
	}
	if forbiddenStatus(status) {
		return exitForbidden
	}
	fmt.Fprintf(os.Stderr, "note: HTTP %d\n%s\n", status, clip(raw))
	return exitFail
}

func forbiddenStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func (e apiEnv) request(method, path string, payload any) (int, []byte, error) {
	var buf io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.endpoint(path), buf)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("JOB-TOKEN", e.Token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := e.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, raw, nil
}

func clip(b []byte) string {
	s := string(b)
	if len(s) > 500 {
		return s[:500]
	}
	return s
}
