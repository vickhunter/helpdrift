# helpdrift

Catch help articles that your last PR made wrong.

helpdrift reads your docs (Markdown/MDX), finds the concrete things they claim
(button labels, CLI flags, env vars, URL paths, config keys, plan names,
prices) and checks them against what your pull request removed or renamed in
the code. It needs no annotations and no LLM, and your code never leaves your
machine or CI.

```text
help/billing/export.md:7  "Export CSV"  [label]
  renamed in web/src/ui/BillingPage.tsx:8  "Export CSV" -> "Download report"
  suggestion: replace "Export CSV" with "Download report"
```

Status: early MVP. A single static Go binary using only the standard library.

## Install

```sh
go install github.com/vickhunter/helpdrift/cmd/helpdrift@latest
# or, from a checkout:
make build        # -> bin/helpdrift
```

## Use

```sh
# In a PR branch: compare the docs against what changed since the base branch
helpdrift check --base origin/main

# Compare two commits without touching the work tree
helpdrift check --base v1.4.0 --head v2.0.0

# Have a patch instead of a git range? Feed the unified diff and point
# --root at the tree with the change applied (a/ b/ prefixes are handled)
helpdrift check --diff change.patch --root .

# Only some docs, machine-readable output, fail CI on findings
helpdrift check --base origin/main --docs 'help/**/*.md,docs/**/*.mdx' \
  --format json --fail-on-findings

# Audit all docs against the current code (no diff): flags and env vars
# mentioned in the docs that no longer exist anywhere in the code
helpdrift scan
helpdrift scan --kinds flag,env,path

# See which claims helpdrift extracts from a file
helpdrift anchors help/billing/export.md
```

`check` diffs from the merge base of `--base` and `--head`, like a pull
request does. Without `--head` it compares against the working tree, so
uncommitted changes count too.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | No findings, or findings without `--fail-on-findings` |
| 1 | Findings and `--fail-on-findings` was set |
| 2 | Usage or runtime error |

### Output formats

`--format text` (default), `--format json` or `--format markdown` (a table
meant for a PR comment). `--output FILE` writes the report to a file;
`--markdown-file FILE` and `--json-file FILE` write extra copies in those
formats in the same run.

## How it works

1. **Claims in the docs.** Each Markdown/MDX file is scanned line by line for
   specific-looking text: **bold** and "quoted" labels, `inline code`, `--flags`,
   `UPPER_SNAKE` env vars, URL paths (from inline code, prose and absolute
   links), keys in YAML/TOML code blocks, prices (`$12`, `€9.99`) and
   quantities (`14-day`, `5 seats`). Fenced code blocks only contribute flags,
   env vars, paths and config keys. Front matter, relative links, HTML tags
   and MDX `import`/`export` lines are skipped.
2. **Changes in the code.** The diff is split into blocks of removed and added
   lines. Each line is tokenized (string literals, JSX/HTML text nodes,
   identifiers, flags, numbers; comments are ignored). Removed lines are paired
   with the added lines they most resemble, and tokens that changed in the same
   position become *renames* (`"Export CSV"` -> `"Download report"`,
   `"output"` -> `"out-file"`) or *value changes* (`priceMonthly: 12` -> `15`).
3. **Matching.** A claim is reported when the diff removed it **and** it no
   longer appears anywhere in the new code (so text that just moved to another
   file is not flagged). Kinds are respected: a bold label only matches string
   literals or UI text, never an identifier; `--output` also matches a bare
   `"output"` literal, which is how most flag libraries declare flags. Labels
   match case-insensitively, everything else exactly.
4. **Findings** carry the doc file and line, the code file and line, the kind of
   change (`renamed`, `removed`, `changed`) and, when the replacement is known,
   a mechanical suggestion.

Docs files themselves (and other prose: `.md`, `.mdx`, `.rst`, `.adoc`, `.txt`)
are never treated as code. If a PR already updated an article, the old claim
is gone from the new docs and nothing is reported.

## Config (`.helpdrift.yml`)

Optional, read from the root of the repository. Flags override it.

```yaml
docs: ["help/**/*.md", "docs/**/*.mdx"]   # default: **/*.md, **/*.mdx
code: ["src/**", "locales/**"]            # default: every non-doc file
exclude: ["src/generated/**"]             # added to built-in excludes
ignore: ["Settings", "Dashboard"]         # claims never reported
min_length: 3
```

Built-in excludes: `.git`, `node_modules`, `vendor`, `dist`, `build`,
`.next`, `.venv`, `__pycache__`, `CHANGELOG*`, `HISTORY*`, `RELEASES*`, lock
files, minified JS, source maps and SVGs. Globs support `**`, `{a,b}` and,
for patterns without a `/`, matching any path segment (`node_modules`,
`*.md`). The file format is a small YAML subset: top-level keys with scalar,
inline-list or block-list values.

## Silencing a claim in a doc

```markdown
---
helpdrift: ignore        # skip this whole file
---

<!-- helpdrift-ignore-line --> Click **Legacy export** (being phased out).

<!-- helpdrift-ignore-start -->
Anything in here is skipped.
<!-- helpdrift-ignore-end -->
```

Or pass `--ignore "Legacy export"` / use `ignore:` in the config.

## GitHub Action

This repository is a composite action that builds helpdrift from source and
runs `helpdrift check` against the pull request's base branch. Check out with
full history so the base branch is available:

```yaml
- uses: actions/checkout@v4
  with:
    fetch-depth: 0
- id: helpdrift
  uses: vickhunter/helpdrift@main
  with:
    docs: "help/**/*.md"
    fail-on-findings: "true"
```

Inputs: `base`, `docs`, `config`, `fail-on-findings`, `args`. Outputs:
`findings` (count), `report` (Markdown file), `json` (JSON file). The Markdown
report is also added to the job summary.

helpdrift never calls the GitHub API. To get a PR comment, post the report from
your own workflow with its `GITHUB_TOKEN`. A complete example is in
[`examples/pull-request-workflow.yml`](examples/pull-request-workflow.yml).

## Optional LLM review

Off by default and never needed. With `--llm`, each finding (up to
`--llm-max`, default 20) is sent with a few lines of the article and the
before/after code lines to any OpenAI-compatible `/chat/completions` endpoint,
including local servers. The model answers `confirm` or `reject` and may
propose a corrected line.

```sh
HELPDRIFT_LLM_BASE_URL=http://localhost:11434/v1 \
HELPDRIFT_LLM_MODEL=qwen3 \
  helpdrift check --base origin/main --llm

# hosted endpoints also need a key
HELPDRIFT_LLM_API_KEY=... HELPDRIFT_LLM_BASE_URL=https://api.example.com/v1 ...
```

The verdict is added to every output format. Findings the model rejects are
still listed but do not count for `--fail-on-findings`. Errors from the
endpoint are recorded per finding and never fail the run. Nothing is applied
or published automatically. Note that with `--llm` the excerpt and code lines
leave your machine for the endpoint you configure.

## Known limitations

- Matching is textual. Behaviour changes that don't touch a literal (a default
  computed in code, a reordered menu) are invisible without the LLM pass.
- UI text must be in the repository (source, i18n JSON/YAML). Text from a CMS,
  a database or a remote translation service can't be checked.
- Tokenizing is line-based: multi-line string literals and labels split across
  lines (other than a bare JSX/HTML text line) are not recognised.
- A renamed claim that still exists elsewhere in the code (an alias, a test
  fixture, an old migration) is not reported. This is deliberate: it trades
  recall for precision. Use `code:`/`exclude:` to narrow what counts.
- Prices and quantities are matched through value changes next to a hinting
  key (`price`, `amount`, `cents`, `days`, `seats`, ...), so unusual key names
  are missed.
- `scan` mode is noisier than `check`, especially for labels and paths, which
  is why it defaults to flags and env vars.
- No SARIF output yet, and `--root` should be the repository root.

## Development

```sh
make check   # gofmt check, go vet, go test
make demo    # build and run against testdata/fixtures/demo in a temp git repo
```

Tests are fixture-based: each directory in `testdata/fixtures/` has a
`before/` and an `after/` tree that the tests commit into a temporary git
repository before running `check`.

## License

MIT. See [LICENSE](LICENSE) and [PROVENANCE.md](PROVENANCE.md).
