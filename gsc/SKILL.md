---
name: gsc
description: Query Google Search Console with the gsc CLI: search performance, top queries and pages, trends, period comparisons, URL inspection, sitemaps, exports, and SEO insights for a property.
---

# Google Search Console (gsc)

Use the installed `gsc` binary; check subcommand `--help` for flags. The full reference is
[commands](../docs/commands.md); login methods are in [auth](../docs/auth.md).

- Prefer `-o json --no-input`. Data is on stdout, diagnostics on stderr. Pass `--read-only` unless the user
  asked for a change.
- Check `gsc auth status -o json` first (local, no API call). If not logged in, ask the user to run
  `gsc auth login`; never invent credentials. For CI, `GSC_CREDENTIALS` or `GSC_ACCESS_TOKEN` may be set.
- Select the property with `-s` (`sc-domain:example.com`, a URL-prefix, or a bare host) or rely on the
  profile default shown by `auth status`.
- Dates are Pacific Time and inclusive. Run `gsc freshness` before trusting the last few days: final data
  usually lags two to three days. Never present `--data-state all` numbers as final.
- Totals come from `gsc performance`. Never sum `query` or `page` rows into site totals: anonymized queries
  are withheld and at most 50,000 rows per day are exposed. `complete: true` only means pagination ended.
- `--compare` returns `null` for rows missing from one period; treat that as unknown, not zero.
  `insights new-queries` and `lost-queries` describe exposure within the fetched rows, not rankings.
- `gsc inspect` reports Google's indexed version, not a live test, and cannot request indexing. It costs
  quota (2,000 per property per day); inspect only the URLs you need and use `--max`.
- Search Analytics has unpublished load quotas. Prefer narrower date ranges and avoid grouping by page and
  query together over long ranges. Do not retry quota errors in a loop.
- Writes (`sites add/remove`, `sitemaps submit/delete`) need the user's explicit request; use `--dry-run`
  to show the request first. Some UI features (branded filter, AI Overviews reports, Insights) have no API.

## Updating gsc

- `gsc update --check -o json` reports `current_version`, `latest_version`, `update_available`,
  `release_url`, and `install_method` (`self`, or `go` for a source build in a Go bin directory).
- Install only when the user asks: `gsc update --yes --no-input` (macOS, Linux, and Windows). It verifies
  the SHA-256 checksum and leaves the old binary in place on any failure; `--read-only` blocks it.
- The once-a-day update notice runs only when stderr is a terminal, so agent runs never see it or trigger
  its GitHub request. `GSC_NO_UPDATE_NOTIFIER=1` or `NO_UPDATE_NOTIFIER=1` turns it off everywhere.
