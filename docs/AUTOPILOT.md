# helpdrift Autopilot

GitLab CI/CD runs the checks. GitLab Duo Agent Platform rewrites the stale
articles and drafts release notes. A person merges. That is the Supervised
loop: the flow may commit and comment, and it does not merge.

The commits on `main` through 4 October 2026 are the original MIT project.
This branch does not rewrite them.

## Stages

| Stage | Job | When it runs |
|---|---|---|
| `test` | `test` | Merge requests, branches, tags. `go test ./...` |
| `docs-drift` | `docs-drift` | Merge requests. `helpdrift check --base $CI_MERGE_REQUEST_DIFF_BASE_SHA --head $CI_COMMIT_SHA` |
| `docs-drift` | `docs-drift-scan` | Scheduled pipelines only. `helpdrift scan`, then an issue when there is drift |
| `secure` | SAST and secret detection | Included templates, moved out of `test` |
| `package` | `package` | Merge requests, the default branch, and tags. Cross-compiled binaries, generic package registry on CI |
| `release` | `release-notes` | Merge requests and the default branch. Notes artifact, no Release |
| `release` | `release` | Tags. GitLab Release |
| `pages` | `pages-preview` | Merge requests. Static site artifact, not published |
| `pages` | `pages` | Default branch and tags. GitLab Pages from `docs/` |

A merge request pipeline has six stages: `test`, `docs-drift`, `secure`, `package`, `release`, `pages`.

## Docs the flows follow

Custom flows use flow registry v1 and only `environment: ambient`. The files under `.gitlab/duo/flows/` use that schema and nothing else. Triggers are not a field in the flow file.

- [Custom flows](https://docs.gitlab.com/user/duo_agent_platform/flows/custom/) — create the flow in **AI > Flows**, paste the YAML, enable it, and add a trigger. Custom flows define their own agents. They do not call a separate custom agent.
- [Custom flow YAML schema](https://docs.gitlab.com/user/duo_agent_platform/flows/custom_flows_schema/) — `version`, `environment`, `components`, `prompts`, `routers`, `flow`, and `coding_environment`. Top-level `name`, `description`, and `product_group` are rejected. `model` inside a prompt is rejected.
- [Flow registry framework v1](https://docs.gitlab.com/user/duo_agent_platform/flows/claude-edit-v1-flow-registry/) — `AgentComponent`, inline prompts (`unit_primitives`, `placeholder: history`), tools, and `flow.inputs` for `agent_platform_standard_context`.
- [Triggers](https://docs.gitlab.com/user/duo_agent_platform/triggers/) — event types and the configuration-path option.
- [Customize Duo](https://docs.gitlab.com/user/duo_agent_platform/customize/) — `.gitlab/duo/` may hold custom flow definitions.
- [Configure flow execution](https://docs.gitlab.com/user/duo_agent_platform/flows/execution/) and [agent-config.yml](https://docs.gitlab.com/user/duo_agent_platform/flows/execution/agent-config-yaml/) — read from the default branch only.

Tool names in the flow files are the ones that page lists: `read_file`, `edit_file`, `create_file_with_contents`, `create_commit`, `create_merge_request_note`, `get_merge_request`, `run_command`, `gitlab_api_get`, `gitlab_merge_request_search`, `get_project`, and the file-search tools.

## docs-drift-fixer

File: [`.gitlab/duo/flows/docs-drift-fixer.yml`](../.gitlab/duo/flows/docs-drift-fixer.yml).

The agent reads `reports/helpdrift.json` (the `docs-drift` job artifact, or a check it runs itself), edits the Markdown files named in the findings, commits them on the workload branch with message `docs: update N claims (helpdrift)`, and posts one merge request note. The human merges.

<!-- helpdrift-ignore-start -->
The articles that the demo rewrites quote this command, and only this command, as a code span:

`helpdrift check --format md`
<!-- helpdrift-ignore-end -->

`scripts/demo-rename.sh` replaces that command in Go files and does not touch `docs/`.

### Trigger (GitLab UI, not YAML)

**TODO.** The flow file cannot name its trigger. In the project, after the flow exists:

1. **AI > Flows > New flow**. Display name `docs-drift-fixer`. Paste the YAML. Visibility private or restricted.
2. Enable the flow in this project.
3. Add a **Merge request** trigger with the **Created** action. That action exists as of GitLab 19.4 ([triggers](https://docs.gitlab.com/user/duo_agent_platform/triggers/)).
4. A per-event Goal is optional and, as documented, sits behind `ai_flow_trigger_goals`, which is disabled by default. The prompt in the YAML is enough without a goal. If the field is available, a goal that fits the docs is: `Update the help articles the helpdrift report marks stale, commit the edits, and post a short summary.`

**TODO.** **Configuration source > Configuration path** can point at `.gitlab/duo/flows/docs-drift-fixer.yml`. The same page says that option is shown only when `ai_catalog_create_third_party_flows` is enabled. Do not depend on it until the option is visible. Pasting the YAML into the catalog is the path the custom-flow page documents without that flag.

A service account runs the flow. GitLab assigns it the Developer role. It must not be able to merge. That is the Supervised gate.

## release-notes

File: [`.gitlab/duo/flows/release-notes.yml`](../.gitlab/duo/flows/release-notes.yml).

The agent writes Markdown notes from merged merge requests when the event payload is a tag pipeline (`refs/tags/`). Otherwise it stops. Its final answer is the notes.

**TODO.** No trigger event type for tags is documented. The event types are Mention, Assign, Assign reviewer, Pipeline events (Running, Passed, Failed, Canceled), Merge request (Approved, Created, Marked ready, Merge conflict), and Work item ([triggers](https://docs.gitlab.com/user/duo_agent_platform/triggers/)). The closest hook is **Pipeline events > Passed**. The prompt ignores a payload that is not a tag. Attach that in the UI if the project should run the agent. Do not invent a `tag` event in the YAML.

**TODO.** No listed tool updates a Release description. Custom flows get `GITLAB_TOKEN` / `GITLAB_OAUTH_TOKEN` limited to the `ai_workflows` scope, and other endpoints are refused ([custom flows](https://docs.gitlab.com/user/duo_agent_platform/flows/custom/)). The tag job `release` therefore publishes notes from `scripts/release-notes.sh`, which reads git history. It is the deterministic fallback, not the Duo draft. When a Release update is confirmed in scope, point the release job at the agent's notes.

The release job uses the `release` keyword and `registry.gitlab.com/gitlab-org/cli:latest`, which authenticates with `CI_JOB_TOKEN` ([`release`](https://docs.gitlab.com/ci/yaml/#release), [Releases](https://docs.gitlab.com/user/project/releases/)). Asset URLs point at the generic package uploaded by `package`.

## CI jobs that call the API

`scripts/ci` sends `JOB-TOKEN: $CI_JOB_TOKEN`.

- `note` creates or updates the merge request note whose body contains the helpdrift marker. The docs-drift job runs it on every merge request.
- `issue` opens an issue titled `docs: helpdrift scan found drift` with label `docs-drift` when the scheduled scan exits 1.

**TODO.** The job token's Notes API access is GET only, and Issues API access is GET only ([CI/CD job token](https://docs.gitlab.com/ci/jobs/ci_job_token/), [fine-grained permissions](https://docs.gitlab.com/ci/jobs/fine_grained_permissions/), [Notes API](https://docs.gitlab.com/api/notes/)). The program still sends POST and PUT. On HTTP 401 or 403 it prints those links and exits 0 so a read-only token does not fail a clean drift check. The Markdown report is always a job artifact. Do not add a project token, personal token, or other secret to the repository to paper over this. If the provisioned project can allow the write without a committed secret, turn that on in the GitLab UI and the same request will start succeeding.

## Other pipeline facts

- SAST: [`Jobs/SAST.gitlab-ci.yml`](https://docs.gitlab.com/user/application_security/sast/). Secret detection: [`Jobs/Secret-Detection.gitlab-ci.yml`](https://docs.gitlab.com/user/application_security/secret_detection/pipeline/). Their jobs are moved to `secure` the way [Security configuration](https://docs.gitlab.com/user/application_security/detect/security_configuration/) describes. `AST_ENABLE_MR_PIPELINES` is set so the scanners are not limited to branch pipelines.
- Packages: [`PUT /projects/:id/packages/generic/:package_name/:package_version/:file_name`](https://docs.gitlab.com/user/packages/generic_packages/) with `JOB-TOKEN`. Tag pipelines use `CI_COMMIT_TAG` as the version. Other pipelines use `0.0.0-$CI_COMMIT_SHORT_SHA`.
- Pages: [`pages: true`](https://docs.gitlab.com/user/project/pages/introduction/) publishes `public/`, which `scripts/build_pages.py` builds from `docs/`. Merge requests only build a preview artifact so they do not replace the live site.
- Scheduled scan: create it under **Build > Pipeline schedules**. The repository cannot create the schedule. A schedule sets `CI_PIPELINE_SOURCE=schedule`, which is the only time `docs-drift-scan` runs. SAST and secret detection are turned off for that pipeline via `workflow:rules:variables`.

## Demo

1. The live article says the Markdown format command quoted above.
2. Run `scripts/demo-rename.sh` (or the same edit in a merge request). It changes Go source only.
3. `docs-drift` fails and writes JSON plus Markdown. The note step posts that Markdown when the API allows it.
4. The docs-drift-fixer flow commits `docs: update N claims (helpdrift)` to the merge request branch.
5. The pipeline runs again. A person merges.
6. A tag runs `package`, then `release`, then `pages`.

Local check, after a commit that still has the old command in Go:

```sh
make build
scripts/demo-rename.sh
./bin/helpdrift check --base HEAD --docs 'docs/**/*.md' --fail-on-findings
git checkout -- cmd/helpdrift/main.go
```

Restore the Go file before committing. The articles stay on the old command until the fixer runs.

## What has to happen once a GitLab project exists

1. Push this branch. Do not rewrite `main`. The 4 October history is the original MIT project. Put the MIT license in the project About description.
2. Make the project public when the hackathon role allows it.
3. Turn on GitLab Duo Agent Platform and custom flows. Merge `.gitlab/duo/agent-config.yml` to the default branch. GitLab ignores that file on other branches.
4. Create and enable **docs-drift-fixer** as above, with a Merge request / Created trigger. Create **release-notes** and, if you want it to run, a Pipeline events / Passed trigger. Both stay Supervised: the service account does not merge.
5. Enable the package registry and Pages. Run one merge request pipeline and one tag pipeline so the history shows the stages. Shared runners need Docker for SAST and secret detection. Confirm the project's tier includes those scanners.
6. Create a pipeline schedule for `docs-drift-scan`.
7. Confirm whether job-token note and issue writes are allowed. If they are not, leave the TODO. Do not commit a token.
8. The Pages URL is the deployed project. Keep it up through the announcement window.

Not in this branch: an issue-triage flow for the `docs-drift` label, and a protected environment or approval gate on Pages. Those need the project tier and the flows above running first.

## Verification recorded with this branch

Commands and results are filled in when this branch is validated. Re-run them after editing the pipeline or the demo articles.

```sh
yamllint .gitlab-ci.yml .gitlab/duo/agent-config.yml .gitlab/duo/flows/docs-drift-fixer.yml .gitlab/duo/flows/release-notes.yml
go test ./...
scripts/demo-rename.sh
./bin/helpdrift check --base HEAD --docs 'docs/**/*.md' --format json --fail-on-findings
git checkout -- cmd/helpdrift/main.go
```
