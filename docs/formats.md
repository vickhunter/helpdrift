# Report formats

`helpdrift check` can print a plain-text log, a JSON document, or a Markdown table.

Use the Markdown table when a merge request should show the stale claims:

`helpdrift check --format md`

JSON is the document the docs-drift fixer reads. Text is the default when you run the command yourself. `--fail-on-findings` exits 1 when the report is not empty, and 0 when every quoted claim still matches the code.
