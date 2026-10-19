# helpdrift

Catch help articles that a merge request made wrong.

These pages are the help center helpdrift guards. A code change that rewrites a command in Go and leaves the articles alone fails the docs-drift gate. The docs-drift fixer then edits the articles and a person merges.

- [Check a merge request](check.md)
- [Report formats](formats.md)
- [Autopilot](AUTOPILOT.md)
