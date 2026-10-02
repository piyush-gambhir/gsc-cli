# Google Search Console API: research

Researched on 2026-10-03 by gpt-6.1-sol (Codex CLI, live web search, read-only) and curated by Claude.
Every claim links to its source, and community sources are labeled as such. No authenticated calls were
made, so behavior marked unverified needs a credentialed smoke test during the build.

Re-checked by Claude against primary sources on 2026-10-03:

- Google's OAuth policy says to "always store encrypted tokens at rest" and "You must never commit client
  credentials into publicly available code repositories."
  ([policies](https://developers.google.com/identity/protocols/oauth2/policies))
- The device authorization flow's scope allowlist contains neither `webmasters` nor `webmasters.readonly`.
  ([device flow](https://developers.google.com/identity/protocols/oauth2/limited-input-device))
- Homebrew's `ghostscript`, `gambit-scheme`, and `gerbil-scheme` formulae all install a `gsc` binary and
  declare conflicts over it; Homebrew's `imagemagick` does not depend on `ghostscript`.
  ([formula API](https://formulae.brew.sh/api/formula/ghostscript.json))

Where this research recommends something different from [PLAN.md](PLAN.md) (binary name, a shared OAuth
client versus bring-your-own, the Indexing API), PLAN.md records the decision and why.

# 1. Summary

Research checked against live documentation and discovery data on **2026-10-03**. I read all eight requested Clarity files first. No repository files were changed.

- **The active Search Console API has 10 methods:** four sites methods, four sitemaps methods, Search Analytics query, and URL Inspection. Its `v1` discovery service combines `/webmasters/v3/...` and `/v1/...` paths. I found no additional active method introduced since 2024. [API reference](https://developers.google.com/webmaster-tools/v1/api_reference_index)
- **Hourly analytics exists:** `hour` with `dataState=hourly_all`, covering up to 10 days. This was added on April 9, 2025. Daily historical data remains the appropriate interface for longer periods. [Hourly API announcement](https://developers.google.com/search/blog/2025/04/san-hourly-data)
- **Analytics pagination is incomplete by design:** maximum 25,000 rows per response and 50,000 exposed rows per day per search type. Page/query detail can lose additional rows; fetching every page does not recover every underlying query. [Extraction guide](https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data)
- **URL Inspection reads Google’s indexed snapshot.** It cannot run a live test or request indexing. Its principal limit is 2,000 inspections per site per day. [Inspection method](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect), [quotas](https://developers.google.com/webmaster-tools/limits)
- **Prefer BYO Desktop OAuth with loopback redirect and PKCE**, plus service-account credentials for automation. Google’s device-flow allowlist excludes both Search Console scopes. [Native OAuth](https://developers.google.com/identity/protocols/oauth2/native-app), [device-flow scopes](https://developers.google.com/identity/protocols/oauth2/limited-input-device)
- **Do not promise that default gcloud ADC works.** Google documents using `--client-id-file` when requesting non-Cloud scopes. The public primary sources do not establish a universal, scope-specific block for `webmasters`. [ADC login reference](https://docs.cloud.google.com/sdk/gcloud/reference/auth/application-default/login)
- **OAuth scope classification needs a release check in Cloud Console.** Current community evidence calls `webmasters.readonly` non-sensitive and `webmasters` sensitive, but conflicting claims exist. Google’s public scope catalog does not certify these labels. External apps in Testing still get seven-day refresh tokens for these scopes. [Scope catalog](https://developers.google.com/identity/protocols/oauth2/scopes), [token expiration rules](https://developers.google.com/identity/protocols/oauth2)
- **Keep OAuth refresh tokens out of plaintext YAML.** Google’s current policy requires encrypted token storage at rest. Preserve Clarity’s XDG profiles, permissions, locking, and atomic writes for metadata, while storing credentials in an OS credential store. [OAuth policy](https://developers.google.com/identity/protocols/oauth2/policies)
- **Exclude the Indexing API from the general CLI.** It is a separate service restricted to qualifying job and livestream pages, with an owner-level service-account setup and abuse enforcement. [Indexing API restrictions](https://developers.google.com/search/apis/indexing-api/v3/using-api), [quickstart policy](https://developers.google.com/search/apis/indexing-api/v3/quickstart)
- **Use raw HTTP plus `golang.org/x/oauth2` for this suite.** Current checked versions are `google.golang.org/api v0.300.0` and `golang.org/x/oauth2 v0.37.0`. The generated Google client is viable, but the small endpoint surface does not justify its broader dependency graph here. [Google releases](https://github.com/googleapis/google-api-go-client/releases), [OAuth package](https://pkg.go.dev/golang.org/x/oauth2)
- **Prefer the binary name `searchconsole`.** `gsc` collides with Gambit and Homebrew Ghostscript; `gsc-cli` already names several Search Console tools; `gscli` is also taken. [Ghostscript formula](https://raw.githubusercontent.com/Homebrew/homebrew-core/master/Formula/g/ghostscript.rb), [community gsc-cli](https://github.com/benedict2310/gsc-cli), [gscli](https://github.com/shaharia-lab/gscli)

# 2. API surface

## Endpoint inventory

Use **`https://searchconsole.googleapis.com`** as the service root. Some method documentation still shows `https://www.googleapis.com`; the current discovery document specifies the dedicated Search Console hostname. Its checked revision is **`20260923`**. [Live discovery](https://www.googleapis.com/discovery/v1/apis/searchconsole/v1/rest)

Scope shorthand:

- **R:** `https://www.googleapis.com/auth/webmasters.readonly`
- **W:** `https://www.googleapis.com/auth/webmasters`

Read methods accept R or W. Write methods require W. OAuth scope and property permission are separate requirements. [Authorization guide](https://developers.google.com/webmaster-tools/v1/how-tos/authorizing)

In the table, `{siteUrl}` and `{feedpath}` are independently escaped path parameters.

| Method | HTTP and path | Key inputs | Returns | Read/write | Scope and permission |
|---|---|---|---|---|---|
| [sites.list](https://developers.google.com/webmaster-tools/v1/sites/list) | `GET /webmasters/v3/sites` | None | `siteEntry[]` | Read | R/W; authenticated user’s property set |
| [sites.get](https://developers.google.com/webmaster-tools/v1/sites/get) | `GET /webmasters/v3/sites/{siteUrl}` | Property identifier | Site resource | Read | R/W; property accessible to principal |
| [sites.add](https://developers.google.com/webmaster-tools/v1/sites/add) | `PUT /webmasters/v3/sites/{siteUrl}` | Property identifier; no body | Empty success | Write | W; adds to authenticated principal’s set |
| [sites.delete](https://developers.google.com/webmaster-tools/v1/sites/delete) | `DELETE /webmasters/v3/sites/{siteUrl}` | Property identifier; no body | Empty success | Write | W; removes from authenticated principal’s set |
| [sitemaps.list](https://developers.google.com/webmaster-tools/v1/sitemaps/list) | `GET /webmasters/v3/sites/{siteUrl}/sitemaps` | Optional `sitemapIndex` query parameter | `sitemap[]` | Read | R/W; property access |
| [sitemaps.get](https://developers.google.com/webmaster-tools/v1/sitemaps/get) | `GET /webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}` | Full sitemap URL | Sitemap resource | Read | R/W; property access |
| [sitemaps.submit](https://developers.google.com/webmaster-tools/v1/sitemaps/submit) | `PUT /webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}` | Full sitemap URL; no body | Empty success | Write | W; full user or owner for submission |
| [sitemaps.delete](https://developers.google.com/webmaster-tools/v1/sitemaps/delete) | `DELETE /webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}` | Full sitemap URL; no body | Empty success | Write | W; sufficient property permission |
| [searchanalytics.query](https://developers.google.com/webmaster-tools/v1/searchanalytics/query) | `POST /webmasters/v3/sites/{siteUrl}/searchAnalytics/query` | JSON query below | Rows, aggregation, optional metadata | Read | R/W; performance-data access |
| [urlInspection.index.inspect](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect) | `POST /v1/urlInspection/index:inspect` | `inspectionUrl`, `siteUrl`, optional `languageCode` | `inspectionResult` | Read | R/W; use full user or owner as conservative setup |
| `urlTestingTools.mobileFriendlyTest.run` | Formerly `POST /v1/urlTestingTools/mobileFriendlyTest:run` | `url`, optional `requestScreenshot` | Formerly test status, issues, screenshot | Retired | Former public testing API used API keys; exclude |

The Mobile-Friendly Test API was retired on **December 1, 2023**, although its method and types remain in current generated schemas. Schema presence is not evidence of an operational API. [Retirement announcement](https://developers.google.com/search/blog/2023/04/page-experience-in-search?hl=en), [generated client](https://raw.githubusercontent.com/googleapis/google-api-go-client/v0.300.0/searchconsole/v1/searchconsole-gen.go)

Google’s permission matrix explicitly grants sitemap submission to owners and full users. Restricted users have fewer inspection capabilities. It does not provide a method-by-method service-account compatibility matrix. [Permissions](https://support.google.com/webmasters/answer/7687615)

## Sites and sitemaps

A site resource contains:

```text
siteUrl
permissionLevel:
  siteOwner
  siteFullUser
  siteRestrictedUser
  siteUnverifiedUser
```

`sites.add` adds a property to the caller’s set; it does not itself verify ownership. `sites.delete` removes that caller’s property entry. These methods are not an ownership-verification or user-management API. [Sites resource](https://developers.google.com/webmaster-tools/v1/sites), [add](https://developers.google.com/webmaster-tools/v1/sites/add), [delete](https://developers.google.com/webmaster-tools/v1/sites/delete)

A sitemap resource contains:

```text
path
lastSubmitted
lastDownloaded
isPending
isSitemapsIndex
type
warnings
errors
contents[]:
  type
  submitted
  indexed   # deprecated
```

Dates are documented as RFC 3339. Sitemap types include `atomFeed`, `rssFeed`, `sitemap`, `urlList`, `notSitemap`, and legacy types. Content types include web, image, video, news, mobile, and legacy app/pattern categories. Preserve unfamiliar values. The discovery schema represents several `int64` counters as JSON strings. [Sitemap resource](https://developers.google.com/webmaster-tools/v1/sitemaps), [discovery schema](https://www.googleapis.com/discovery/v1/apis/searchconsole/v1/rest)

Neither sites nor sitemaps list documents pagination. Sitemap submission supplies a URL, not XML content. Deleting a sitemap entry does not instruct Google to stop crawling it or remove its URLs from the index. Do not present deprecated `contents[].indexed` as current index coverage. [List](https://developers.google.com/webmaster-tools/v1/sitemaps/list), [submit](https://developers.google.com/webmaster-tools/v1/sitemaps/submit), [delete](https://developers.google.com/webmaster-tools/v1/sitemaps/delete)

## Search Analytics request

The complete request field set is:

```json
{
  "startDate": "2026-09-01",
  "endDate": "2026-09-30",
  "dimensions": ["date", "device"],
  "type": "web",
  "dataState": "final",
  "aggregationType": "auto",
  "dimensionFilterGroups": [
    {
      "groupType": "and",
      "filters": [
        {
          "dimension": "query",
          "operator": "includingRegex",
          "expression": "example"
        }
      ]
    }
  ],
  "rowLimit": 25000,
  "startRow": 0
}
```

`startDate` and `endDate` are required, inclusive `YYYY-MM-DD` dates. `searchType` is a deprecated predecessor of `type`; use `type`. Other fields are optional. [Request reference](https://developers.google.com/webmaster-tools/v1/searchanalytics/query), [generated request type](https://pkg.go.dev/google.golang.org/api/searchconsole/v1#SearchAnalyticsQueryRequest)

### Dimensions

| Dimension | Meaning | Grouping | Filter dimension |
|---|---|---:|---:|
| `date` | Pacific calendar date | Yes | No |
| `hour` | Pacific hour with explicit UTC offset | Yes | No |
| `query` | Search query text | Yes | Yes |
| `page` | Page URI, with canonical attribution | Yes | Yes |
| `country` | ISO 3166-1 alpha-3 country | Yes | Yes |
| `device` | `DESKTOP`, `MOBILE`, `TABLET` | Yes | Yes |
| `searchAppearance` | Search-result feature | Yes, special handling | Yes |

Dimensions form the row key in request order. Duplicate dimensions are invalid. No dimensions requests an aggregate row. Hour keys use an offset date-time format such as `2026-10-02T13:00:00-07:00`. [Discovery dimension definitions](https://www.googleapis.com/discovery/v1/apis/searchconsole/v1/rest)

**Search appearance needs a two-step extraction:** query `searchAppearance` alone, then filter one returned appearance value while requesting other dimensions. The extraction guide says it cannot be a grouping column alongside other dimensions, despite the method reference’s broader statement about arbitrary dimension combinations. Follow the more specific guide and document this discrepancy. [Extraction guide](https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data)

### Search type and data state

| Field | Values | Interpretation |
|---|---|---|
| `type` | `web` (default), `image`, `video`, `news`, `discover`, `googleNews` | `news` is Google Search’s News tab; `googleNews` is news.google.com and the Google News apps |
| `dataState` | `final` (default) | Finalized data |
|  | `all` | Finalized and preliminary data |
|  | `hourly_all` | Hourly data, including preliminary data |

`web` is the combined All-tab report and excludes Discover and Google News. The documented data-state spellings are case-insensitive. `hour` requires `hourly_all`, and hourly availability is at most 10 days. [Method reference](https://developers.google.com/webmaster-tools/v1/searchanalytics/query), [hourly announcement](https://developers.google.com/search/blog/2025/04/san-hourly-data)

Discovery represents many enums in uppercase underscore form, while method documentation uses lower/camel-case spellings. Expose the method-reference spellings in CLI flags; retain returned strings without coercing them. [Discovery](https://www.googleapis.com/discovery/v1/apis/searchconsole/v1/rest)

### Aggregation and invalid combinations

| `aggregationType` | Meaning | Documented restrictions |
|---|---|---|
| `auto` | Service chooses | Recommended when grouping/filtering by page |
| `byPage` | Aggregate by canonical URI | Different counting from property aggregation |
| `byProperty` | Aggregate by property | Invalid with page grouping or filtering; unsupported for `discover` and `googleNews` |
| `byNewsShowcasePanel` | Aggregate by News Showcase panel | Requires `searchAppearance=NEWS_SHOWCASE` and `type=discover` or `googleNews`; incompatible with page grouping/filtering or another appearance filter |

An explicitly invalid aggregation causes an error; the API does not silently substitute a valid aggregation. Additional documented invalid inputs include duplicate dimensions, negative `startRow`, out-of-range `rowLimit`, unsupported filter dimensions, and `or` filter groups. [Query reference](https://developers.google.com/webmaster-tools/v1/searchanalytics/query)

The documentation does **not** provide a complete compatibility matrix for every dimension, filter operator, and search type. Avoid inventing additional client-side prohibitions.

### Filters

All filter groups must match, and each supported group uses **`and`**. Although prose mentions an OR concept, the reference says it is not supported.

| Operator | Semantics |
|---|---|
| `equals` | Default; exact match, case-sensitive for `page` and `query` |
| `notEquals` | Negated exact match, same case rule |
| `contains` | Case-insensitive substring or full match |
| `notContains` | Negated case-insensitive substring/full match |
| `includingRegex` | Matches an RE2 expression |
| `excludingRegex` | Does not match an RE2 expression |

Filters can target dimensions not included in grouping. The reference states a **4,096-character maximum**, but attaches it imprecisely to the filter list rather than clearly to `expression`. Treat this as a documented filter-length limit and flag the precise enforcement unit as unverified. [Filter reference](https://developers.google.com/webmaster-tools/v1/searchanalytics/query)

Do not substitute UI filtering behavior for API behavior: API exact page/query matching is explicitly case-sensitive.

### Response, pagination, and completeness

```text
rows[]:
  keys[]        # follows requested dimension order
  clicks
  impressions
  ctr           # fraction, 0..1
  position
responseAggregationType
metadata?:
  firstIncompleteDate?
  firstIncompleteHour?
```

The metric fields are JSON numbers. Results normally sort by descending clicks, with arbitrary tie order; grouping by date sorts chronologically. Missing dates are omitted rather than returned as zero rows. [Response reference](https://developers.google.com/webmaster-tools/v1/searchanalytics/query), [generated response types](https://raw.githubusercontent.com/googleapis/google-api-go-client/v0.300.0/searchconsole/v1/searchconsole-gen.go)

| Limit | Value |
|---|---:|
| Default `rowLimit` | 1,000 |
| Allowed `rowLimit` | 1 through 25,000 |
| Default `startRow` | 0 |
| Pagination | Zero-based offset, no continuation token |
| Offset beyond results | Successful empty response |
| Exposed row ceiling | 50,000 per day per search type |

Google’s extraction recipe increments `startRow` by 25,000 until a zero-row response. The ceiling is a data-exposure limit, not a request quota. Longer ranges and repeated filtered queries do not establish that the underlying dataset has been completely recovered. [Extraction guide](https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data)

**Metadata discrepancy:** the method page documents `first_incomplete_date` and `first_incomplete_hour`; discovery and the current Go client use camel-case JSON fields. Support both when decoding, and preserve the raw response. Date metadata requires `all` plus date grouping; hour metadata requires `hourly_all` plus hour grouping. These identify the start of potentially changing data. [Method metadata](https://developers.google.com/webmaster-tools/v1/searchanalytics/query), [Go JSON tags](https://raw.githubusercontent.com/googleapis/google-api-go-client/v0.300.0/searchconsole/v1/searchconsole-gen.go)

## URL Inspection

Request:

```json
{
  "inspectionUrl": "https://example.com/article",
  "siteUrl": "sc-domain:example.com",
  "languageCode": "en-US"
}
```

`inspectionUrl` and `siteUrl` are required. The URL must belong to the supplied property. `languageCode` is optional, uses BCP 47, and defaults to `en-US`. The method returns the indexed version’s status only. [Inspect method](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect)

Response field map:

| Object | Fields |
|---|---|
| `inspectionResult` | `inspectionResultLink`, `indexStatusResult`, `ampResult`, `mobileUsabilityResult` (deprecated), `richResultsResult` |
| `indexStatusResult` | `verdict`, `coverageState`, `robotsTxtState`, `indexingState`, `lastCrawlTime`, `pageFetchState`, `googleCanonical`, `userCanonical`, `referringUrls[]`, `sitemap[]`, `crawledAs` |
| `ampResult` | `verdict`, `ampUrl`, `robotsTxtState`, `indexingState`, `ampIndexStatusVerdict`, `lastCrawlTime`, `pageFetchState`, `issues[]` |
| AMP issue | `issueMessage`, `severity` |
| `richResultsResult` | `verdict`, `detectedItems[]` |
| Detected rich-result type | `richResultType`, `items[]` |
| Rich-result item | `name`, `issues[]` |
| Rich-result issue | `issueMessage`, `severity` |
| Deprecated mobile result | `verdict`, `issues[]` containing issue type, severity, message |

Fields are conditional. Sitemap/referrer lists are not exhaustive, and absent canonical or crawl fields need not indicate a parser failure. Crawl timestamps are RFC 3339 timestamps. [Result schema](https://developers.google.com/webmaster-tools/v1/urlInspection.index/UrlInspectionResult)

Important enums include:

- Verdict: `VERDICT_UNSPECIFIED`, `PASS`, `PARTIAL` (reserved), `FAIL`, `NEUTRAL`.
- Robots: unspecified, `ALLOWED`, `DISALLOWED`.
- Indexing: unspecified, `INDEXING_ALLOWED`, `BLOCKED_BY_META_TAG`, `BLOCKED_BY_HTTP_HEADER`, reserved robots blocking.
- Fetch: success, soft 404, robots blocking, not found, access denied/forbidden, server/redirect/other 4xx/internal crawl errors, invalid URL.
- Crawler: unspecified, `DESKTOP`, `MOBILE`.
- Issue severity: unspecified, `WARNING`, `ERROR`.

Keep enums open for future additions. [Generated enum definitions](https://raw.githubusercontent.com/googleapis/google-api-go-client/v0.300.0/searchconsole/v1/searchconsole-gen.go)

It cannot:

- Fetch and test the current live page.
- Request indexing or recrawling.
- Return a property-wide Page Indexing report.
- Manage ownership, users, removals, links reports, or crawl-statistics reports.

These operations are absent from the current API surface. An inspection result can deep-link to the UI, but a CLI must not describe it as a live test. [Inspect method](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect), [API inventory](https://developers.google.com/webmaster-tools/v1/api_reference_index)

## Separate Indexing API

This is **`indexing.googleapis.com`**, not Search Console.

| Method | HTTP/path | Input and result |
|---|---|---|
| `urlNotifications.publish` | `POST /v3/urlNotifications:publish` | `{url,type}` where type is `URL_UPDATED` or `URL_DELETED`; returns notification metadata |
| `urlNotifications.getMetadata` | `GET /v3/urlNotifications/metadata?url=...` | Returns latest notification metadata, not index status |
| Batch transport | `POST /batch` | Up to 100 calls per multipart batch; underlying requests still count |

Metadata includes `url`, `latestUpdate`, and `latestRemove`, with notification type/time. Acceptance is not an indexing guarantee. Official eligibility is pages containing **`JobPosting`**, or **`BroadcastEvent` embedded in `VideoObject`**. [Using the API](https://developers.google.com/search/apis/indexing-api/v3/using-api)

The documented setup uses a service account added as a **delegated owner** of the verified Search Console property, with scope `https://www.googleapis.com/auth/indexing`. This owner requirement must not be confused with ordinary Search Console service-account access. [Prerequisites](https://developers.google.com/search/apis/indexing-api/v3/prereqs)

Recommendation: exclude it from this CLI. A future purpose-specific tool could enforce eligible content and its separate permissions. A generic `request-indexing` command would invite unsupported use; Google documents abuse detection and possible access revocation. [Quickstart](https://developers.google.com/search/apis/indexing-api/v3/quickstart)

## Adjacent APIs, outside the initial scope

**PageSpeed Insights:** `GET https://www.googleapis.com/pagespeedonline/v5/runPagespeed` runs Lighthouse-based analysis of a supplied URL. An API key is optional but recommended for frequent use. Google says it plans to stop including CrUX field data in PSI responses, without specifying a retirement date on the checked page. Treat it as a separate performance CLI or later integration. [PSI guide](https://developers.google.com/speed/docs/insights/v5/get-started)

**CrUX:** `POST https://chromeuxreport.googleapis.com/v1/records:queryRecord` returns real Chrome-user experience data for an origin or URL, optionally by form factor. It uses an API key, a rolling 28-day collection window, daily updates, and a documented 150 QPM per-project limit. It has separate coverage and semantics from Search Console. [CrUX API](https://developer.chrome.com/docs/crux/api)

**Bulk export to BigQuery:** owners configure daily exports in the Search Console UI. It avoids the ordinary API row ceiling and permits longer archival retention, but does not backfill the full historical Search Console window. Tables retain anonymized aggregate rows without disclosing query text; the schema page inconsistently describes that text as null versus empty. Setup, billing, and SQL belong outside the initial CLI. [Bulk export announcement](https://developers.google.com/search/blog/2023/02/bulk-data-export), [setup](https://support.google.com/webmasters/answer/12917675), [table schema](https://support.google.com/webmasters/answer/12917991?hl=en)

# 3. Authentication options

## Scopes and verification

| Scope | Access | Current classification conclusion |
|---|---|---|
| `webmasters.readonly` | Read access, including inspection | **Not definitively verified from a public primary source.** Current community evidence says non-sensitive; conflicting community material says sensitive |
| `webmasters` | Read/write access | **Not definitively verified from a public primary source.** Current community evidence says sensitive |
| Either scope restricted? | Would imply stricter verification | No primary-source confirmation found that either is restricted |

Google’s scope catalog describes access but directs developers to Cloud Console for sensitivity indicators. The appropriate release gate is to inspect **Google Auth Platform → Data Access** in the actual project and record both classifications. [Official scope catalog](https://developers.google.com/identity/protocols/oauth2/scopes), [verification guidance](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification)

The discrepancy is real: a September 2026 Google developer-forum participant calls readonly non-sensitive; a current community specification calls it sensitive. Another integration provider calls readonly non-sensitive and write sensitive. These are **community claims**, not Google policy certifications. [Forum report](https://discuss.google.dev/t/verification-center-stuck-prepare-for-verification-disabled-branding-page-has-no-verify-branding-or-publish-branding-button-pii-removed-by-staff/397135), [conflicting specification](https://github.com/ajmalaksar25/searchlight/blob/main/SPEC.md), [integration disclosure](https://outdoos.com/integrations/google/)

| Policy concept | Meaning for this CLI |
|---|---|
| Non-sensitive scope | No sensitive-scope review solely because of that scope; production branding requirements can still apply |
| Sensitive scope | Public production use generally requires scope justification, consent/demo review, and app verification |
| Restricted scope | Additional requirements; a security assessment can apply depending on how restricted data is handled |
| Unverified-app cap | Apps subject to the unverified warning are limited to 100 new users in total, not 100 daily |
| External app in Testing | Refresh tokens expire after seven days unless only basic identity scopes are requested |
| In Production | Removes that Testing-specific expiry; does not make refresh tokens permanent |

Both Search Console scopes exceed the basic identity-only exception, regardless of their sensitivity label. Personal-use and organization-internal exceptions are distinct from operating a distributable public app. [Sensitive verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification), [restricted verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/restricted-scope-verification), [unverified apps](https://support.google.com/cloud/answer/7454865), [refresh-token rules](https://developers.google.com/identity/protocols/oauth2)

## Installed-app OAuth: recommended interactive method

**End-user setup:**

1. Create or select a Google Cloud project and enable Search Console API.
2. Configure the OAuth audience and branding. For personal testing, add the user as a test user.
3. Create a **Desktop app** OAuth client and download its JSON.
4. Run a proposed command such as:

```sh
searchconsole auth login \
  --profile personal \
  --client-id-file /path/to/desktop-client.json
```

5. Authorize R by default. Request W only through an explicit write-enabled login.

Implementation should open the system browser, use an ephemeral `http://127.0.0.1:<port>` loopback listener, random state, PKCE S256, and offline access. Exchange the code at `https://oauth2.googleapis.com/token`. Do not use an embedded browser or restore the deprecated out-of-band copy/paste flow. [Native-app OAuth](https://developers.google.com/identity/protocols/oauth2/native-app)

**Shared client versus BYO:** a maintainer-owned native OAuth registration is technically possible; BYO is not a Search Console API requirement. Native applications cannot keep a client secret confidential. However, Google’s general policy also forbids committing client credentials to public repositories. A shared registration therefore needs deliberate release provisioning, quota ownership, branding, policy review, and verification where applicable. Recommend BYO for v0.1, with a shared client considered later. [Native client model](https://developers.google.com/identity/protocols/oauth2/native-app), [credential policy](https://developers.google.com/identity/protocols/oauth2/policies)

| Choice | Advantages | Costs |
|---|---|---|
| BYO Desktop client | Quota isolation; no central consent-project dependency; suitable for early open-source release | Cloud Console setup; Testing expiry can surprise users |
| Maintainer client | Easier onboarding | Shared quota, verification and branding work, operational responsibility, policy/distribution questions |

## Device authorization flow: exclude

Google allows device authorization only for an explicit scope allowlist. **Neither `webmasters.readonly` nor `webmasters` appears on it.** A GitHub-style device-code login cannot be assumed to work for Google Search Console. [Device-flow documentation](https://developers.google.com/identity/protocols/oauth2/limited-input-device)

For headless environments, use service accounts, explicit ADC, impersonation, federation, or externally supplied tokens.

## Service accounts: recommended automation method

**End-user setup:**

1. Enable Search Console API in the service account’s project.
2. Create a service account.
3. Add its `client_email` in the property’s **Settings → Users and permissions**.
4. Grant full-user access for inspection and sitemap operations, or a narrower role where sufficient.
5. Supply a credential file explicitly, or impersonate that service account.

A service account obtains an OAuth access token using its credentials; it does not authenticate Search Console requests with an API key. Google’s Search Console quickstart recognizes service-account use, while the permission matrix determines property capabilities. [Quickstart](https://developers.google.com/webmaster-tools/v1/quickstart/quickstart-python), [service-account OAuth](https://developers.google.com/identity/protocols/oauth2/service-account), [property permissions](https://support.google.com/webmasters/answer/7687615)

**Method coverage:** URL Inspection and sitemap submission use the same OAuth scopes and property authorization model as other active methods. There is no documented service-account exclusion for them. This supports using a suitably authorized service account, but I did not execute authenticated calls to certify every method. `sites.add` does not substitute for manually granting property access.

**Domain-wide delegation:** a Workspace administrator can authorize the required scopes and let a service account impersonate an organization user. The impersonated user must still have property access. Delegation does not automatically confer access to every Search Console property, and direct service-account property access does not require it. [Delegation documentation](https://developers.google.com/identity/protocols/oauth2/service-account)

## Application Default Credentials

Google documents using an application’s own OAuth client when requesting non-Cloud scopes:

```sh
gcloud auth application-default login \
  --client-id-file=/path/to/desktop-client.json \
  --scopes=https://www.googleapis.com/auth/webmasters.readonly
```

Then use an explicit CLI credential mode such as `--auth adc`. `gcloud auth login` and `gcloud auth application-default login` serve different credential consumers. [ADC login reference](https://docs.cloud.google.com/sdk/gcloud/reference/auth/application-default/login), [ADC setup](https://docs.cloud.google.com/docs/authentication/provide-credentials-adc)

**Does Google currently block the default gcloud client specifically for webmasters?** Public primary documentation does not establish that universal claim. It does establish the custom-client workaround for non-Cloud scopes. Document that supported path and avoid making the default client a dependency.

ADC is useful for users already familiar with Google authentication, but implicit discovery can unexpectedly choose machine or environment credentials. Make it opt-in.

## API keys

**API keys alone cannot authorize any active Search Console property method.** The authorization guide requires OAuth. Its separate API-key discussion concerns the now-retired public Testing Tools API. Generic generated `WithAPIKey` helpers and the query documentation’s `?key=` example do not override the OAuth requirement. [Authorization guide](https://developers.google.com/webmaster-tools/v1/how-tos/authorizing)

## Federation and impersonation for CI

The useful keyless design is:

```text
CI OIDC identity
  → Workload Identity Federation / STS
  → service-account impersonation
  → access token with webmasters scope
  → Search Console
```

Grant Search Console access to the impersonated service account’s email. IAM Credentials `generateAccessToken` accepts requested OAuth scopes. Direct federation principals are not ordinary email users that can simply be added in Search Console. This architecture follows the documented federation and impersonation model; its Search Console integration remains an implementation smoke test. [Federation](https://docs.cloud.google.com/iam/docs/workload-identity-federation), [access-token generation](https://docs.cloud.google.com/iam/docs/reference/credentials/rest/v1/projects.serviceAccounts/generateAccessToken)

Recommend ordinary service-account credentials and BYO OAuth in v0.1, explicit ADC next, then federation/impersonation integration.

## Comparable tools and token handling

| Tool | Useful precedent | Limitation of comparison |
|---|---|---|
| gcloud | ADC, explicit scopes, custom-client override | Its first-party Cloud OAuth client is not a general authorization shortcut |
| rclone | Documents user-created OAuth clients | Its current Drive documentation warns about retirement of the shared client during 2026; this is rclone-specific |
| gh | Browser/device login, OS credential store, external token support | GitHub device-flow support says nothing about Google’s scope allowlist |

Sources: [gcloud](https://docs.cloud.google.com/sdk/gcloud/reference/auth/application-default/login), [rclone](https://rclone.org/drive/#making-your-own-client-id), [gh login](https://cli.github.com/manual/gh_auth_login).

**Recommended storage and refresh behavior:**

- XDG YAML stores profiles, default property, auth mode, client/credential references, and granted scopes.
- Store refresh tokens in an OS credential store. Google requires encrypted tokens at rest; `0600` alone is not encryption.
- Refresh only when an explicitly invoked API command needs a valid token. Keep `auth status` local unless `--verify`.
- Preserve the existing refresh token when a refresh response omits a replacement.
- Return a useful `invalid_grant` error requiring reauthorization; do not repeatedly retry.
- Keep access-token flag/environment support for externally managed credentials.
- Under `--read-only`, permit read-request token acquisition in memory, but block persistent credential/config mutations.

These are recommendations adapted to Clarity’s config and auth behavior, with the encryption requirement from [Google policy](https://developers.google.com/identity/protocols/oauth2/policies).

# 4. Quotas, limits, errors

## Search Console request quotas

All applicable limits apply simultaneously. “Not published” means the checked quota page does not supply that dimension.

| Resource group | Per site | Per user | Per project |
|---|---|---|---|
| Search Analytics | **1,200 QPM** | **1,200 QPM** | **40,000 QPM**, **30,000,000 QPD** |
| URL Inspection | **600 QPM**, **2,000 QPD** | Not separately published | **15,000 QPM**, **10,000,000 QPD** |
| Other resources, including sites/sitemaps | Not separately published | **20 QPS**, **200 QPM** | **100,000,000 QPD** |

QPS means queries per second; QPM per minute; QPD per day. Source: [Search Console usage limits](https://developers.google.com/webmaster-tools/limits).

### Search Analytics load quota

There are additional **short-term, 10-minute** and **long-term, one-day** load quotas. Google does not publish numeric load budgets.

Grouping/filtering by page or query, particularly both, and querying longer date ranges consume more load. Google advises waiting 15 minutes after a short-term failure; failure of a single query after that can indicate long-term exhaustion. Do not translate load quota into an invented request count or assume every `quotaExceeded` identifies the same budget. [Load limits](https://developers.google.com/webmaster-tools/limits)

## Data limits, separate from request quotas

| Limit | Value |
|---|---|
| Analytics response rows | 25,000 maximum |
| Analytics exposed rows | 50,000 per day per search type |
| Hourly history | Up to 10 days |
| Performance history | 16 months |
| Filter length | Reference states 4,096 characters; enforcement unit ambiguous |

Sources: [extraction limits](https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data), [hourly availability](https://developers.google.com/search/blog/2025/04/san-hourly-data), [retention guidance](https://developers.google.com/search/docs/monitor-debug/debugging-search-traffic-drops), [filter reference](https://developers.google.com/webmaster-tools/v1/searchanalytics/query).

## Indexing API quotas

| Limit | Default |
|---|---:|
| Publish requests per project per day | **200**, combined update/delete |
| Metadata requests per project per minute | **180** |
| All requests per project per minute | **380** |
| Requests per batch | **100** |
| Daily publish reset | Midnight Pacific Time |

The initial quota is for onboarding/testing; Google documents requesting approval and additional quota, evaluated against eligible content and quality. The API is free, but approval and eligibility still apply. [Quota/pricing](https://developers.google.com/search/apis/indexing-api/v3/quota-pricing), [batch documentation](https://developers.google.com/search/apis/indexing-api/v3/using-api)

## Errors and throttling

The documented Google JSON error structure is:

```json
{
  "error": {
    "code": 403,
    "message": "Quota exceeded.",
    "errors": [
      {
        "domain": "global",
        "reason": "quotaExceeded",
        "message": "Quota exceeded."
      }
    ]
  }
}
```

Entries can also contain location information. Preserve the complete envelope and unfamiliar reasons. [Search Console errors](https://developers.google.com/webmaster-tools/v1/errors)

| HTTP status | Relevant interpretation |
|---|---|
| 400 | Invalid parameters, combinations, or request body |
| 401 | Missing, expired, or invalid authorization |
| 403 | Insufficient permission, disabled API, or quota/rate failure |
| 404 | Missing resource or invalid resource identifier |
| 429 | Rate limiting |
| 5xx | Server/backend failure |

Relevant reasons include `rateLimitExceeded`, `userRateLimitExceeded`, `quotaExceeded`, and daily-limit errors. Rate/quota failures can occur as **403 or 429**, so status alone is insufficient. [Error reference](https://developers.google.com/webmaster-tools/v1/errors)

A tolerant client should also understand Google’s newer `error.status` and `error.details` shape, such as `RESOURCE_EXHAUSTED`, without assuming every Search Console response uses it. [Google error model](https://google.aip.dev/193)

**Throttling signals:** Search Console does not document guaranteed `Retry-After`, remaining-quota, or reset headers. If `Retry-After` is present, parse and report it. Preserve suite behavior: no automatic request retries, including on 403, 429, and 5xx. OAuth token acquisition before a request is separate from retrying that request.

# 5. Data semantics and gotchas

| Topic | Semantics and implementation consequence |
|---|---|
| Date timezone | Analytics dates are Pacific Time, using daylight-saving offsets. Use `America/Los_Angeles`, not fixed UTC−8 |
| Discovery inconsistency | Some discovery descriptions still say PST/UTC−8, while current method documentation says UTC−7/−8 |
| Date range | Inclusive `YYYY-MM-DD`; validate start ≤ end |
| Daily freshness | Extraction documentation says data is typically available after 2–3 days |
| Recent data | Preliminary daily/hourly data arrives sooner and can change |
| Retention | 16 months of performance history; hourly data has a separate 10-day window |
| Empty dates | Date-grouped responses omit dates without data |
| Identifiers | URL-prefix property: exact URL, commonly with trailing slash. Domain property: `sc-domain:example.com` |

Sources: [date reference](https://developers.google.com/webmaster-tools/v1/searchanalytics/query), [discovery](https://www.googleapis.com/discovery/v1/apis/searchconsole/v1/rest), [freshness guide](https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data), [retention](https://developers.google.com/search/docs/monitor-debug/debugging-search-traffic-drops), [property identifiers](https://developers.google.com/webmaster-tools/v1/sites/get).

For API calls, preserve identifiers returned by `sites.list`. Do not normalize HTTP to HTTPS, remove a path prefix, or convert URL-prefix properties into domain properties. Escape the entire identifier as one path parameter. Newly introduced platform properties need separate compatibility confirmation.

## Metrics

| Metric | Meaning |
|---|---|
| Clicks | Clicks from Google results to the property/page, with counting rules specific to result type and aggregation |
| Impressions | Exposure of a link/result under Google’s component-specific visibility rules |
| CTR | Clicks divided by impressions; API returns a fraction |
| Position | Average of the topmost applicable position per impression; generally one-based in Search reporting |

Ordinary result links, carousels, Discover, images, and other features have different impression rules. Position is not the average ranking of every URL appearing in a result set. Property aggregation can count an impression once where page aggregation counts multiple pages. [Metric definitions](https://support.google.com/webmasters/answer/7042828), [aggregation differences](https://support.google.com/webmasters/answer/17011364)

Performance data is generally attributed to Google’s selected canonical URL, which may differ from the URL visited or supplied as a page filter. Consequently, Search Console clicks are not interchangeable with analytics sessions. [Metric attribution](https://support.google.com/webmasters/answer/7042828)

For client aggregation, recompute CTR from summed counts. Weight position by impressions only when combining compatible, disjoint rows. Never average row CTRs or positions without weighting, and never label a sum of truncated query rows as the complete property total.

## Anonymized queries and row loss

Rare queries are withheld to protect privacy. They can contribute to unfiltered totals while disappearing from query rows. Applying a query filter also excludes anonymized queries, so complementary query filters need not sum to the unfiltered total. Separately, Google keeps only important/top rows and may discard more detail for page/query combinations. [Filtering deep dive](https://developers.google.com/search/blog/2022/10/performance-data-deep-dive)

This is documented privacy suppression and internal row limitation. Google does not publish a sampling percentage that would justify scaling returned rows to estimate missing totals.

## Why API and UI totals differ

| Cause | Practical comparison rule |
|---|---|
| Query privacy suppression | Compare unfiltered aggregates, not summed query rows |
| Property versus page aggregation | Match aggregation explicitly |
| API row limits versus UI table limits | Distinguish chart totals from downloaded/displayed rows |
| Preliminary versus final data | Match freshness state and comparison time |
| Timezone differences | The UI’s 24-hour view uses local time; API date/hour data uses Pacific time |
| Search type and appearance | Match report, type, filters, and supported dimensions |
| Canonical attribution | Compare the same property and canonical page interpretation |
| Logging corrections or missing data | Check Google’s anomaly log before attributing changes to the CLI |

Sources: [discrepancies](https://support.google.com/webmasters/answer/17010575), [recent-data UI](https://developers.google.com/search/blog/2024/12/recent-data-search-console), [anomalies](https://support.google.com/webmasters/answer/6211453).

AI Overviews and AI Mode data contributes to overall Performance reporting. The 2026 dedicated generative-AI views do not establish a corresponding public API filter or dimension. [AI features](https://developers.google.com/search/docs/appearance/ai-features), [2026 reporting announcement](https://developers.google.com/search/blog/2026/06/gen-ai-performance-reports)

# 6. Recent changes (2024–2026)

| Date | Change | CLI consequence |
|---|---|---|
| **2024, following the November 8, 2023 announcement** | `GOOD_PAGE_EXPERIENCE` appearance API support was scheduled for removal after 180 days | Treat appearance values as evolving; no current support assumption. [Announcement](https://developers.google.com/search/blog/2023/04/page-experience-in-search?hl=en) |
| **2024-12-12** | New 24-hour UI view, hourly presentation, and improved freshness | UI local-time comparisons need care. [Announcement](https://developers.google.com/search/blog/2024/12/recent-data-search-console) |
| **2025-04-09** | Hourly Search Analytics API data, `hour`, `hourly_all`, up to 10 days | Include hourly support in v0.1. [Announcement](https://developers.google.com/search/blog/2025/04/san-hourly-data) |
| **2025-05-21** | Google published guidance covering AI Overviews and AI Mode performance | These contribute to Search reporting; do not invent an API search type. [Guidance](https://developers.google.com/search/blog/2025/05/succeeding-in-ai-search) |
| **2025-06-12, updated 2025-09-09** | Retirement of several structured-data report types: Course Info, Claim Review, Estimated Salary, Learning Video, Special Announcement, Vehicle Listing | API support was retained only through December 2025; avoid fixed appearance inventories. [Announcement](https://developers.google.com/search/blog/2025/06/simplifying-search-results) |
| **2025-06-30** | Search Console Insights moved into the main Search Console experience | No new public Insights method found. [Announcement](https://developers.google.com/search/blog/2025/06/search-console-insights) |
| **2025-10-27** | Query groups added to Insights | UI grouping is not a new API dimension. [Announcement](https://developers.google.com/search/blog/2025/10/search-console-query-groups) |
| **2025-11-20; rollout update 2026-03-11** | Branded/non-branded query filter, using Google classification and eligibility conditions | No corresponding documented API field found. Regex approximation should be labelled client-defined. [Announcement](https://developers.google.com/search/blog/2025/11/search-console-branded-filter?hl=en) |
| **2025-12-10** | Weekly and monthly UI views | CLI can aggregate compatible daily data locally; no new endpoint required. [Announcement](https://developers.google.com/search/blog/2025/12/weekly-monthly-views-search-console) |
| **2026-06-03; worldwide rollout 2026-08-31** | Dedicated Search Generative AI performance reports for Search and Discover | Includes AI Overviews/AI Mode visibility; no public discovery equivalent found. [Announcement](https://developers.google.com/search/blog/2026/06/gen-ai-performance-reports) |
| **2026-07-07** | Platform properties for Instagram, TikTok, X, YouTube content | API identifiers and compatibility remain unverified. [Announcement](https://developers.google.com/search/blog/2026/07/search-console-social-video-platforms) |
| **2026-09-24** | Web multimodal reporting for Lens, Circle to Search, uploaded images, Chrome image search | No separate type/dimension in checked discovery; do not equate this with existing `image` type. [Announcement](https://developers.google.com/search/blog/2026/09/web-multimodal-in-sc) |

Additional 2026 data-quality issues worth documenting:

- An impression-logging issue affected data from **2025-05-13 through 2026-04-27**, with corrected impressions and related metrics; clicks were unaffected.
- Some **February 28–March 1, 2026** bulk-export data was missing and unrecoverable.
- August 2026 entries record Discover and generative-AI reporting gaps and restoration work.

Use the maintained [data anomalies log](https://support.google.com/webmasters/answer/6211453) when validating historical comparisons.

**Version/deprecation conclusion:** no new active Search Console resource method or public API version was found for 2024–2026. Hourly data extended an existing method. The checked `v1` service still contains old `webmasters/v3` paths and retired Mobile-Friendly types. [Current inventory](https://developers.google.com/webmaster-tools/v1/api_reference_index), [live discovery](https://www.googleapis.com/discovery/v1/apis/searchconsole/v1/rest)

# 7. Existing tools

## Official tooling

I found **no dedicated, officially supported Google Search Console CLI or MCP server** in the checked official documentation and repositories. This is a bounded search result, not proof that none exists anywhere. Google documents API clients and samples. [Official quickstart](https://developers.google.com/webmaster-tools/v1/quickstart/quickstart-python)

The Google Workspace CLI repository is not evidence of an official Search Console CLI: its README explicitly says it is not an officially supported Google product, and its main scope is Workspace. [Repository](https://github.com/googleworkspace/cli)

## Community tools

Capabilities below are README/source claims, not independently exercised behavior.

| Project | Coverage and authentication | Relevance |
|---|---|---|
| [benedict2310/gsc-cli](https://github.com/benedict2310/gsc-cli) | Go, service accounts; sites, analytics, inspection, sitemap reads, reporting helpers | Closest small Go precedent; read-oriented |
| [dannolan/gsc-cli](https://github.com/dannolan/gsc-cli) | Go, standard-library approach; service-account file/environment/external credential command; analytics and inspection | Demonstrates a small raw-HTTP design |
| [cyrilghali/gsc-cli](https://github.com/cyrilghali/gsc-cli) | TypeScript; BYO OAuth/browser login and service accounts; GSC and Trends commands | Auth/profile precedent; different runtime and scope |
| [msclabs/gsc-cli](https://github.com/msclabs/gsc-cli) | TypeScript; analytics, inspection, sites/sitemaps, MCP, additional output formats, Indexing integration | Broad surface; avoid copying general Indexing exposure |
| [awkoy/gsc-cli](https://github.com/awkoy/gsc-cli) | TypeScript, structured output, gcloud/ADC-based authentication | Shows why ADC requirements need precise documentation |
| [surendranb/google-search-console-mcp](https://github.com/surendranb/google-search-console-mcp) | Python MCP; analytics, sites, inspection, sitemap reads/writes; service-account configuration | Broad MCP coverage; README “real-time” inspection and indexed-count wording exceeds current API guarantees |
| [jurgisgavenas/search-console-mcp](https://github.com/jurgisgavenas/search-console-mcp) | Read-oriented analytics, sites, sitemaps, inspection | Useful agent-tool packaging precedent |
| [Vrealmatic/gsc-mcp-server](https://github.com/Vrealmatic/gsc-mcp-server) | OAuth readonly access, refresh credentials, multi-site support | Interactive OAuth precedent |

Our differentiation should be predictable suite behavior: Go binary, named profiles, explicit auth selection, complete analytics parameters including hour, documented incompleteness, accurate inspection terminology, no background network traffic, and enforced read-only annotations.

## Go libraries and dependency choice

| Option | Checked current version/status | Advantages | Costs |
|---|---|---|---|
| `google.golang.org/api/searchconsole/v1` | Module **v0.300.0**, released **2026-10-01** | Generated request/response types, parameter escaping, OAuth/ADC integration |
| Same library | Officially supported, maintenance-mode framework; discovery clients still regenerated | Less manual schema maintenance | Broad shared auth/transport dependency graph; retains retired API types |
| `golang.org/x/oauth2` | **v0.37.0**, published **2026-08-25** | PKCE helpers, token sources, Google credential support | Endpoint/DTO/error handling remains ours |
| Raw `net/http` + `encoding/json` + OAuth | Recommended | Small API surface; precise transport, output, logging, and retry behavior | Need fixtures and deliberate schema updates |

Sources: [Google releases](https://github.com/googleapis/google-api-go-client/releases), [Search Console package](https://pkg.go.dev/google.golang.org/api/searchconsole/v1), [OAuth package](https://pkg.go.dev/golang.org/x/oauth2), [OAuth module](https://raw.githubusercontent.com/golang/oauth2/v0.37.0/go.mod).

Recent Google module releases are approximately weekly or biweekly, with occasional shorter intervals, largely driven by discovery regeneration. This is an observed cadence, not an SLA. The module moved to Go 1.26 minimum in September 2026, matching the suite. [Release history](https://github.com/googleapis/google-api-go-client/releases)

If using the generated client, specify readonly scope explicitly: its default constructor uses all available scopes. Its generic API-key constructor does not make Search Console key-authenticated. [Client options](https://pkg.go.dev/google.golang.org/api/searchconsole/v1)

**Binary size:** the Google module declares a substantially broader dependency graph than OAuth alone, including shared Google auth, gax, gRPC/protobuf, and telemetry dependencies. Unused service packages are not all linked merely because they share a module. No defensible MB difference was measured in this read-only research session. Compare matched builds later using identical Go version, target, stripping flags, and functionality. [Google module manifest](https://raw.githubusercontent.com/googleapis/google-api-go-client/main/go.mod), [Go build behavior](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies)

Recommendation: retain the suite’s existing Cobra/config/output dependencies and add OAuth, rather than the complete generated API module.

# 8. Binary name check

| Candidate | Verified collisions | Assessment |
|---|---|---|
| `gsc` | Gambit compiler; Homebrew Ghostscript also installs it; existing GSC tools | High risk |
| `gsc-cli` | Several community Search Console CLI projects | Better than `gsc`, but existing PATH/package-name competition |
| `gscli` | Existing Google Service CLI for Gmail/Drive/Calendar | Avoid |
| `searchconsole` | No prominent executable collision found in this search | Preferred, subject to packaging checks |
| `searchconsole-cli` | More descriptive and namespaced | Good repository/formula alternative |
| `piyush-gsc` | More owner-specific | Low ambiguity, less ergonomic |

**Actual Homebrew evidence:** the current Ghostscript formula explicitly conflicts with Gambit and Gerbil because both install `gsc`. Gambit’s formula also declares Ghostscript/Gerbil conflicts. This is an actual file collision, not merely a similar formula name. [Ghostscript formula](https://raw.githubusercontent.com/Homebrew/homebrew-core/master/Formula/g/ghostscript.rb), [Gambit formula](https://github.com/Homebrew/homebrew-core/blob/c4537752f605ac9c9c3bf467b2cb2f7b1dbe4433/Formula/g/gambit-scheme.rb)

**Linux distinction:** Debian’s Gambit package installs `/usr/bin/gsc`; the checked Debian Ghostscript package does not. Thus “Ghostscript always ships `gsc` on Linux” would be incorrect. [Gambit file list](https://packages.debian.org/trixie/amd64/gambc/filelist), [Ghostscript file list](https://packages.debian.org/trixie/amd64/ghostscript/filelist)

Other collisions: [community `gsc-cli`](https://github.com/benedict2310/gsc-cli), [existing `gscli`](https://github.com/shaharia-lab/gscli).

**Recommendation:** repository/formula `searchconsole-cli`, executable `searchconsole`. Document an optional user-created `alias gsc=searchconsole`; do not install an unconditional `gsc` alias. No global namespace clearance or complete Homebrew-core absence check was established for the alternatives.

# 9. Recommendations for the CLI (moved)

The report's CLI recommendations were folded into [PLAN.md](PLAN.md), which records each decision and its reason.

# 10. Open questions / unverified

1. **Exact current sensitivity labels:** confirm both scopes in the actual Cloud Console project. Public primary documentation does not label them individually; community sources conflict.
2. **Default gcloud client blocking:** custom-client ADC is documented. A universal webmasters-specific block was not established from primary sources.
3. **Authenticated wire behavior:** no credentials were used. Service-account access for every method, metadata spelling, and some parameter combinations remain smoke-test items.
4. **Complete compatibility matrix:** Google does not publish all dimension/operator/type combinations. Implement documented restrictions and preserve server validation errors.
5. **Filter length:** the 4,096-character limit’s precise enforcement unit is unclear.
6. **2026 UI additions:** branded classification, query groups, generative-AI views, multimodal breakdown, and platform-property identifiers have no confirmed public API equivalent in the checked schema.
7. **Quota headers and reset details:** Search Console does not promise quota-remaining/reset headers or `Retry-After`. Do not infer guarantees.
8. **Binary impact:** no matched build-size benchmark was performed.
9. **Names:** verified collisions are real; alternatives have not received exhaustive package/namespace clearance.
10. **BigQuery schema wording:** anonymized query text is described as both null and empty in the same official page. It is undisclosed in either case.

# 11. Sources

Primary sources unless marked community. Method links are grouped where they cover one resource family.

1. [Search Console API reference](https://developers.google.com/webmaster-tools/v1/api_reference_index): active resource inventory.
2. [Live discovery document](https://www.googleapis.com/discovery/v1/apis/searchconsole/v1/rest): service root, revision, methods, schemas, enums.
3. [Sites list](https://developers.google.com/webmaster-tools/v1/sites/list): list request and response.
4. [Sites get](https://developers.google.com/webmaster-tools/v1/sites/get): property identifiers and read scopes.
5. [Sites add](https://developers.google.com/webmaster-tools/v1/sites/add): add semantics and write scope.
6. [Sites delete](https://developers.google.com/webmaster-tools/v1/sites/delete): caller-property removal.
7. [Sites resource](https://developers.google.com/webmaster-tools/v1/sites): permission values.
8. [Sitemaps list](https://developers.google.com/webmaster-tools/v1/sitemaps/list): index filtering and response.
9. [Sitemaps get](https://developers.google.com/webmaster-tools/v1/sitemaps/get): individual sitemap lookup.
10. [Sitemaps submit](https://developers.google.com/webmaster-tools/v1/sitemaps/submit): URL submission.
11. [Sitemaps delete](https://developers.google.com/webmaster-tools/v1/sitemaps/delete): report-entry deletion.
12. [Sitemaps resource](https://developers.google.com/webmaster-tools/v1/sitemaps): fields and deprecated indexed count.
13. [Search Analytics query](https://developers.google.com/webmaster-tools/v1/searchanalytics/query): parameters, operators, aggregation, response.
14. [Performance extraction guide](https://developers.google.com/webmaster-tools/v1/how-tos/all-your-data): pagination, row ceilings, appearance extraction, latency.
15. [URL Inspection method](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect): indexed-only request behavior.
16. [Inspection result schema](https://developers.google.com/webmaster-tools/v1/urlInspection.index/UrlInspectionResult): response fields.
17. [Usage limits](https://developers.google.com/webmaster-tools/limits): request and load quotas.
18. [API errors](https://developers.google.com/webmaster-tools/v1/errors): statuses and error reasons.
19. [Google error model](https://google.aip.dev/193): modern status/details envelopes.
20. [Authorization guide](https://developers.google.com/webmaster-tools/v1/how-tos/authorizing): OAuth requirements and legacy testing-key distinction.
21. [Native OAuth](https://developers.google.com/identity/protocols/oauth2/native-app): Desktop flow, loopback and PKCE.
22. [Limited-input device OAuth](https://developers.google.com/identity/protocols/oauth2/limited-input-device): scope allowlist.
23. [OAuth scope catalog](https://developers.google.com/identity/protocols/oauth2/scopes): scope descriptions and classification guidance.
24. [Sensitive-scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification): review requirements.
25. [Restricted-scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/restricted-scope-verification): additional review requirements.
26. [Unverified apps](https://support.google.com/cloud/answer/7454865): warning and user cap.
27. [OAuth token lifecycle](https://developers.google.com/identity/protocols/oauth2): Testing expiry and revocation conditions.
28. [OAuth policies](https://developers.google.com/identity/protocols/oauth2/policies): encrypted tokens, credential handling, production branding.
29. [Service-account OAuth](https://developers.google.com/identity/protocols/oauth2/service-account): JWT and domain-wide delegation.
30. [Search Console permissions](https://support.google.com/webmasters/answer/7687615): property roles and capabilities.
31. [Search Console quickstart](https://developers.google.com/webmaster-tools/v1/quickstart/quickstart-python): official client and service-account context.
32. [gcloud ADC login](https://docs.cloud.google.com/sdk/gcloud/reference/auth/application-default/login): scopes and custom-client workaround.
33. [ADC setup](https://docs.cloud.google.com/docs/authentication/provide-credentials-adc): local credentials and non-Cloud authentication.
34. [Workload Identity Federation](https://docs.cloud.google.com/iam/docs/workload-identity-federation): keyless external identities.
35. [IAM token generation](https://docs.cloud.google.com/iam/docs/reference/credentials/rest/v1/projects.serviceAccounts/generateAccessToken): scoped impersonated access tokens.
36. [Indexing API usage](https://developers.google.com/search/apis/indexing-api/v3/using-api): methods, batches and content restrictions.
37. [Indexing prerequisites](https://developers.google.com/search/apis/indexing-api/v3/prereqs): delegated-owner service account.
38. [Indexing quotas](https://developers.google.com/search/apis/indexing-api/v3/quota-pricing): numerical limits and approval.
39. [Indexing quickstart](https://developers.google.com/search/apis/indexing-api/v3/quickstart): eligibility and abuse policy.
40. [PSI guide](https://developers.google.com/speed/docs/insights/v5/get-started): Lighthouse API and planned CrUX removal.
41. [CrUX API](https://developer.chrome.com/docs/crux/api): field-data API, window and quotas.
42. [BigQuery export announcement](https://developers.google.com/search/blog/2023/02/bulk-data-export): bulk-export scope.
43. [BigQuery export setup](https://support.google.com/webmasters/answer/12917675): UI configuration.
44. [BigQuery table reference](https://support.google.com/webmasters/answer/12917991?hl=en): retention, anonymization and schema.
45. [Search metric definitions](https://support.google.com/webmasters/answer/7042828): clicks, impressions, position and attribution.
46. [Aggregation differences](https://support.google.com/webmasters/answer/17011364): property versus page counting.
47. [Data discrepancies](https://support.google.com/webmasters/answer/17010575): API/UI comparison causes.
48. [Performance filtering deep dive](https://developers.google.com/search/blog/2022/10/performance-data-deep-dive): query privacy and row limitations.
49. [Traffic-drop debugging](https://developers.google.com/search/docs/monitor-debug/debugging-search-traffic-drops): retention and archival guidance.
50. [Page-experience announcement](https://developers.google.com/search/blog/2023/04/page-experience-in-search?hl=en): Mobile-Friendly retirement and appearance removal.
51. [Recent-data announcement](https://developers.google.com/search/blog/2024/12/recent-data-search-console): 24-hour UI and freshness.
52. [Hourly API announcement](https://developers.google.com/search/blog/2025/04/san-hourly-data): hour/data-state additions.
53. [AI Search guidance](https://developers.google.com/search/blog/2025/05/succeeding-in-ai-search): AI reporting context.
54. [AI features documentation](https://developers.google.com/search/docs/appearance/ai-features): Performance inclusion.
55. [Structured-data simplification](https://developers.google.com/search/blog/2025/06/simplifying-search-results): report/API retirements.
56. [Insights announcement](https://developers.google.com/search/blog/2025/06/search-console-insights): main-product Insights integration.
57. [Query groups](https://developers.google.com/search/blog/2025/10/search-console-query-groups): Insights grouping.
58. [Branded filter](https://developers.google.com/search/blog/2025/11/search-console-branded-filter?hl=en): classification and rollout.
59. [Weekly/monthly views](https://developers.google.com/search/blog/2025/12/weekly-monthly-views-search-console): UI aggregation.
60. [Generative-AI reports](https://developers.google.com/search/blog/2026/06/gen-ai-performance-reports): dedicated reports and global rollout.
61. [Platform properties](https://developers.google.com/search/blog/2026/07/search-console-social-video-platforms): new property category.
62. [Multimodal reporting](https://developers.google.com/search/blog/2026/09/web-multimodal-in-sc): September 2026 reporting addition.
63. [Data anomalies](https://support.google.com/webmasters/answer/6211453): corrections and missing-data incidents.
64. [Google Go releases](https://github.com/googleapis/google-api-go-client/releases): current version, cadence and Go minimum.
65. [Search Console Go package](https://pkg.go.dev/google.golang.org/api/searchconsole/v1): maintenance status and scope defaults.
66. [Generated Go source](https://raw.githubusercontent.com/googleapis/google-api-go-client/v0.300.0/searchconsole/v1/searchconsole-gen.go): exact JSON tags and retained legacy types.
67. [Google module manifest](https://raw.githubusercontent.com/googleapis/google-api-go-client/main/go.mod): dependency graph.
68. [OAuth Go package](https://pkg.go.dev/golang.org/x/oauth2): current version and PKCE/token helpers.
69. [OAuth module manifest](https://raw.githubusercontent.com/golang/oauth2/v0.37.0/go.mod): Go minimum and dependencies.
70. [Go build documentation](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies): compiled package dependencies.
71. [rclone client setup](https://rclone.org/drive/#making-your-own-client-id): comparable project’s BYO guidance.
72. [gh auth login](https://cli.github.com/manual/gh_auth_login): comparable credential-store behavior.
73. [Google Workspace CLI](https://github.com/googleworkspace/cli): support-status disclaimer.
74. [benedict2310/gsc-cli](https://github.com/benedict2310/gsc-cli): community Go CLI.
75. [dannolan/gsc-cli](https://github.com/dannolan/gsc-cli): community minimal Go implementation.
76. [cyrilghali/gsc-cli](https://github.com/cyrilghali/gsc-cli): community OAuth CLI.
77. [msclabs/gsc-cli](https://github.com/msclabs/gsc-cli): community CLI/MCP.
78. [awkoy/gsc-cli](https://github.com/awkoy/gsc-cli): community ADC CLI.
79. [Search Console MCP](https://github.com/surendranb/google-search-console-mcp): community MCP coverage.
80. [search-console-mcp](https://github.com/jurgisgavenas/search-console-mcp): community read-oriented MCP.
81. [Vrealmatic MCP](https://github.com/Vrealmatic/gsc-mcp-server): community OAuth MCP.
82. [Scope-classification forum report](https://discuss.google.dev/t/verification-center-stuck-prepare-for-verification-disabled-branding-page-has-no-verify-branding-or-publish-branding-button-pii-removed-by-staff/397135): community evidence, not policy.
83. [Searchlight specification](https://github.com/ajmalaksar25/searchlight/blob/main/SPEC.md): conflicting community scope claim.
84. [OutDo integration disclosure](https://outdoos.com/integrations/google/): community readonly/write classification claims.
85. [Ghostscript Homebrew formula](https://raw.githubusercontent.com/Homebrew/homebrew-core/master/Formula/g/ghostscript.rb): actual `gsc` conflict.
86. [Gambit Homebrew formula](https://github.com/Homebrew/homebrew-core/blob/c4537752f605ac9c9c3bf467b2cb2f7b1dbe4433/Formula/g/gambit-scheme.rb): reciprocal executable conflicts.
87. [Debian Gambit files](https://packages.debian.org/trixie/amd64/gambc/filelist): `/usr/bin/gsc`.
88. [Debian Ghostscript files](https://packages.debian.org/trixie/amd64/ghostscript/filelist): distribution-specific distinction.
89. [gscli](https://github.com/shaharia-lab/gscli): existing candidate-name collision.
