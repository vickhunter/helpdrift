# Check a merge request

helpdrift compares the help articles in the repository with the code a merge request changed.

Run this from the repository root to write a Markdown report:

`helpdrift check --format md`

Add `--fail-on-findings` in CI so the pipeline stops when an article still describes something the change removed or renamed.

The report lists the file and line of each stale claim, the code change that caused it, and a suggestion. JSON for the same run is what the docs-drift fixer reads.
