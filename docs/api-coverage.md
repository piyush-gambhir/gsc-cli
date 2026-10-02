# API coverage

Every method in the pinned Google API snapshots, mapped to the `gsc` commands that call it, or listed as
skipped with the reason. The snapshots (discovery documents, revision `20260923`, captured 2026-10-03) are
vendored in `cli-go/internal/coverage/testdata/`; see [compatibility.md](compatibility.md) for hashes and the
refresh procedure.

This page is checked by `TestAPICoverage` (`cli-go/cmd/coverage_test.go`): the test fails if a snapshot
method is missing from the coverage table, if a mapped command does not exist, or if this page does not
list a method or command. Summary: **10 of 10 active Search Console methods implemented**, 1 retired method
skipped, 2 Indexing API methods skipped by decision.

## Search Console API v1

Host `https://searchconsole.googleapis.com`. Read methods accept either OAuth scope; write methods need the
full `webmasters` scope.

| Method | HTTP and path | Status | Commands |
| --- | --- | --- | --- |
| `webmasters.sites.list` | `GET webmasters/v3/sites` | Implemented | `gsc sites list` (also used once after login and to resolve bare hosts) |
| `webmasters.sites.get` | `GET webmasters/v3/sites/{siteUrl}` | Implemented | `gsc sites get` |
| `webmasters.sites.add` | `PUT webmasters/v3/sites/{siteUrl}` | Implemented (write) | `gsc sites add` |
| `webmasters.sites.delete` | `DELETE webmasters/v3/sites/{siteUrl}` | Implemented (write) | `gsc sites remove` |
| `webmasters.sitemaps.list` | `GET webmasters/v3/sites/{siteUrl}/sitemaps` | Implemented | `gsc sitemaps list` |
| `webmasters.sitemaps.get` | `GET webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}` | Implemented | `gsc sitemaps get` |
| `webmasters.sitemaps.submit` | `PUT webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}` | Implemented (write) | `gsc sitemaps submit` |
| `webmasters.sitemaps.delete` | `DELETE webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}` | Implemented (write) | `gsc sitemaps delete` |
| `webmasters.searchanalytics.query` | `POST webmasters/v3/sites/{siteUrl}/searchAnalytics/query` | Implemented (read, despite POST) | `gsc query`, `gsc performance`, `gsc top queries`, `gsc top pages`, `gsc top countries`, `gsc top devices`, `gsc top appearance`, `gsc trend`, `gsc freshness`, `gsc export`, `gsc insights striking-distance`, `gsc insights low-ctr`, `gsc insights cannibalization`, `gsc insights decliners`, `gsc insights new-queries`, `gsc insights lost-queries` |
| `searchconsole.urlInspection.index.inspect` | `POST v1/urlInspection/index:inspect` | Implemented (read, despite POST) | `gsc inspect` |
| `searchconsole.urlTestingTools.mobileFriendlyTest.run` | `POST v1/urlTestingTools/mobileFriendlyTest:run` | Retired, skipped | none. Google retired the Mobile-Friendly Test API on 2023-12-01; the method is still in the discovery document but no longer operates. |

Every request field of `searchanalytics.query` is reachable from `gsc query`: dimensions (`date`, `hour`,
`query`, `page`, `country`, `device`, `searchAppearance`), `type`, `dataState` (`final`, `all`,
`hourly_all`), `aggregationType`, `dimensionFilterGroups` with all six operators, `rowLimit`, and
`startRow` (pagination). `--request-file` accepts a raw body for anything flags do not express.

`gsc api METHOD PATH` can call any of these methods directly, and any method Google adds later.

## Indexing API v3 (skipped by decision)

Host `https://indexing.googleapis.com`, a separate service from Search Console.

| Method | HTTP and path | Status | Reason |
| --- | --- | --- | --- |
| `indexing.urlNotifications.publish` | `POST v3/urlNotifications:publish` | Skipped | Google limits the Indexing API to pages with `JobPosting` or `BroadcastEvent` (in `VideoObject`) structured data, requires an owner-level service account, and enforces against misuse. A general "request indexing" command would invite unsupported use. |
| `indexing.urlNotifications.getMetadata` | `GET v3/urlNotifications/metadata` | Skipped | Same service and restrictions as above. |

If a site you own qualifies, the plan (PLAN.md, decision 4) describes adding it later with a structured-data
eligibility check.

## Related Google APIs and data sources

| Source | How it is handled |
| --- | --- |
| PageSpeed Insights API (`pagespeedonline/v5/runPagespeed`) | Not covered. It analyzes any URL with Lighthouse, uses an API key, and is unrelated to Search Console property data. A separate performance tool fits it better. |
| Chrome UX Report (CrUX) API | Not covered. Real-user field data with an API key and its own quotas; different semantics from Search Console. |
| Search Console bulk data export to BigQuery | Not covered. Owners turn it on in the Search Console UI; the data is then queried in BigQuery (`bq`). It avoids the API's row limits but does not backfill history. `gsc export` is the API-side alternative. |
| Search Console URL Inspection live test, "Request indexing" | Not available in any public API. `gsc inspect` reports the indexed version only. |

## Search Console features without an API (as of the snapshot)

These appear in the Search Console UI but have no public API method or field, so `gsc` cannot report them:

- Branded and non-branded query filter (2025-11, worldwide rollout 2026-03). A regex filter on `query` can
  approximate it, but that is your classification, not Google's.
- Query groups in Search Console Insights (2025-10) and the Insights report itself (2025-06).
- Generative AI performance reports for AI Overviews and AI Mode in Search and Discover (2026-06, worldwide
  2026-08-31). This traffic is included in ordinary performance totals but cannot be separated.
- Web multimodal reporting for Lens, Circle to Search, and image search (2026-09).
- Platform properties for Instagram, TikTok, X, and YouTube (2026-07). They work only as far as
  `gsc sites list` returns them; their identifiers are not documented for the API.
- Weekly and monthly UI views (2025-12). `gsc trend --by week` and `--by month` roll up daily rows locally
  instead.
- Core Web Vitals, Page indexing, Manual actions, Security issues, Links, Removals, Crawl stats, and user
  and permission management reports.
