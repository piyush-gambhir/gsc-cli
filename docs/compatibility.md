# Compatibility and API snapshot

`gsc` is built and tested against a pinned snapshot of Google's API definitions. This page records exactly
which snapshot, where it came from, and how to move to a newer one.

## Pinned snapshot

Captured **2026-10-03** from Google's live discovery service. The files are vendored unchanged in
`cli-go/internal/coverage/testdata/`, and `TestAPICoverage` checks their SHA-256 and revision on every test
run.

| API | Discovery revision | Source | Vendored file | SHA-256 |
| --- | --- | --- | --- | --- |
| Search Console API v1 | `20260923` | `https://searchconsole.googleapis.com/$discovery/rest?version=v1` | `searchconsole-v1.discovery.json` | `4de75278f7d4599b5f437d7b24dbb1957fa5c333d445945de97b3c6bdc4dc322` |
| Indexing API v3 (mapped as skipped) | `20260923` | `https://indexing.googleapis.com/$discovery/rest?version=v3` | `indexing-v3.discovery.json` | `aed76b26295ecd5c5ef71aadcd02d404c633b13641ef7eb5b06a52386e30514d` |

The Search Console discovery document lists 11 methods: the 10 active ones `gsc` implements and the retired
Mobile-Friendly Test. Its service root is `https://searchconsole.googleapis.com/`; older method pages still
show `https://www.googleapis.com/`, which `gsc` does not use. See [api-coverage.md](api-coverage.md) for the
method-by-method mapping.

## Documentation the behavior is based on

Checked on 2026-10-03 (full source list in [RESEARCH.md](../RESEARCH.md)):

| Topic | Page |
| --- | --- |
| Method reference and request fields | https://developers.google.com/webmaster-tools/v1/searchanalytics/query |
| Extraction guidance (pagination, 50,000 rows per day, search appearance) | https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data |
| Quotas (Search Analytics 1,200 QPM per site; URL Inspection 2,000 per day and 600 per minute per site) | https://developers.google.com/webmaster-tools/limits |
| Hourly data (`hour`, `hourly_all`, 10 days; added 2025-04-09) | https://developers.google.com/search/blog/2025/04/san-hourly-data |
| URL Inspection | https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect |
| Errors | https://developers.google.com/webmaster-tools/v1/errors |
| Native-app OAuth (loopback, PKCE) | https://developers.google.com/identity/protocols/oauth2/native-app |
| Device flow scope allowlist (excludes Search Console) | https://developers.google.com/identity/protocols/oauth2/limited-input-device |
| OAuth policies (encrypted token storage, no credentials in public repositories) | https://developers.google.com/identity/protocols/oauth2/policies |
| Data anomalies log (use before comparing history) | https://support.google.com/webmasters/answer/6211453 |

## Known differences and choices

- **Metadata spelling.** The method reference spells the incompleteness fields `first_incomplete_date` and
  `first_incomplete_hour`; discovery and Google's Go client use camelCase. `gsc` accepts both and always
  prints snake_case in its own envelope.
- **Search appearance.** The method reference allows any dimension combination; the extraction guide says
  `searchAppearance` cannot be grouped with other dimensions. `gsc` warns instead of rejecting.
- **Retention.** 16 months (10 days for hourly data) is data availability, not a request rule: explicit
  dates beyond it produce a warning, and `--last` and comparison periods are clipped with a notice.
- **Enums.** Discovery lists some enums in upper case; `gsc` sends the method-reference spellings
  (`byPage`, `hourly_all`) and accepts any case on input. Returned values are preserved as Google sends them.
- **Retired types.** The generated schemas still include Mobile-Friendly Test types; `gsc` does not call them.
- **Data corrections.** Google corrected an impression-logging issue affecting 2025-05-13 to 2026-04-27 and
  recorded other gaps in 2026. Check the anomalies log before attributing a historical change to the CLI.

## Dependencies

Go 1.26 minimum, toolchain 1.27.1. Direct modules (checked 2026-10-03): `spf13/cobra` 1.10.2,
`spf13/pflag` 1.0.10, `gofrs/flock` 0.13.1, `google/renameio/v2` 2.0.2, `go.yaml.in/yaml/v3` 3.0.5,
`golang.org/x/oauth2` 0.37.0 (including `oauth2/google` for service accounts and ADC),
`zalando/go-keyring` 0.2.8, `golang.org/x/term` 0.46.0, `golang.org/x/sys` 0.48.0. The generated
`google.golang.org/api/searchconsole/v1` client is intentionally not used.

## Refreshing the snapshot

1. Download the current documents:
   ```bash
   cd cli-go/internal/coverage/testdata
   curl -fsSL 'https://searchconsole.googleapis.com/$discovery/rest?version=v1' -o searchconsole-v1.discovery.json
   curl -fsSL 'https://indexing.googleapis.com/$discovery/rest?version=v3' -o indexing-v3.discovery.json
   shasum -a 256 *.json
   ```
2. Update the revision and SHA-256 values in `cli-go/internal/coverage/coverage.go` and in the table above.
3. Run `make test`. `TestAPICoverage` fails for every new, removed, or changed method. For each one, implement
   it (and add it to `coverage.Entries`) or add a skipped entry with a reason, then update
   [api-coverage.md](api-coverage.md).
4. Check Google Search Central's blog and the method pages for behavior changes that discovery does not show
   (quotas, retention, new dimensions or data states), and record them in RESEARCH.md.
