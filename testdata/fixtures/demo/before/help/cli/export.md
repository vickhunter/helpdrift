# Exporting from the command line

```bash
export ACME_API_KEY=sk_live_example
acme export --output invoices.csv --since 7d
```

Accounts created before 2024 can authenticate with `ACME_LEGACY_TOKEN` instead.
Use `--since` to limit the time range.
