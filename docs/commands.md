# gsc command reference

Generated from the command tree. Run `make docs` to refresh.

Global flags apply to every command.

```text
      --access-token string   Use this OAuth access token (prefer GSC_ACCESS_TOKEN)
      --credentials string    Google credentials JSON: service account, authorized user, or external account (or GSC_CREDENTIALS)
      --dry-run               For write commands: print the request and send nothing
      --no-input              Never prompt or open a browser
  -o, --output string         Output format: table, json, yaml, csv (default "table")
      --profile string        Named profile (or GSC_PROFILE)
  -q, --quiet                 Suppress informational stderr output
      --read-only             Block remote writes, local credential changes, and self-update
  -s, --site string           Property: sc-domain:example.com, https://www.example.com/, or a bare host (or GSC_SITE)
      --timeout duration      HTTP request timeout (default 30s)
  -v, --verbose               Log request method, URL, and status to stderr (never tokens or bodies)
  -y, --yes                   Confirm destructive commands and updates without prompting
```

## gsc

Google Search Console from your terminal

Search performance, URL inspection, sitemaps, and properties from Google Search Console.
Run `gsc auth login` once; it opens your browser and remembers the login.
Data goes to stdout and diagnostics to stderr. --read-only blocks every write.

```text
gsc
```

## gsc api

Send an authenticated request to the Search Console API (escape hatch)

PATH is relative to https://searchconsole.googleapis.com and must already be URL-escaped.
GET and the read-only POSTs (searchAnalytics/query, urlInspection/index:inspect) are reads; every other
request is treated as a write: blocked by --read-only, printed instead of sent with --dry-run, and
confirmed interactively (or with --yes) before it is sent.

```text
gsc api METHOD PATH [flags]
```

```bash
  gsc api GET /webmasters/v3/sites
  gsc api POST /webmasters/v3/sites/sc-domain%3Aexample.com/searchAnalytics/query --data '{"startDate":"2026-09-01","endDate":"2026-09-30"}'
  gsc api PUT /webmasters/v3/sites/sc-domain%3Aexample.com/sitemaps/https%3A%2F%2Fexample.com%2Fsitemap.xml --dry-run
```

```text
      --data string   JSON request body, @file, or - for stdin
```

## gsc auth

Log in, inspect credentials, and manage profiles

```text
gsc auth
```

## gsc auth list

List saved profiles (no secrets)

```text
gsc auth list
```

```bash
  gsc auth list
  gsc auth list -o json
```

## gsc auth login

Sign in with Google (opens your browser) or save another credential type

With no flags, opens Google's consent page in your browser and saves the login in the OS keychain.
Tokens refresh automatically afterwards. --no-browser prints a URL to approve on any device and
asks you to paste the final redirected URL back. --service-account and --adc save non-interactive
credentials for CI. See docs/auth.md for every method.

```text
gsc auth login [flags]
```

```bash
  gsc auth login
  gsc auth login --profile work --scope readonly
  gsc auth login --no-browser
  gsc auth login --client-secret-file client_secret.json
  gsc auth login --profile ci --service-account key.json
  gsc auth login --profile gcloud --adc --impersonate reporting@my-project.iam.gserviceaccount.com
```

```text
      --adc                         Use Application Default Credentials (gcloud, workload identity federation, metadata server)
      --client-secret-file string   Use your own Desktop OAuth client (JSON from Google Cloud Console)
      --impersonate string          With --adc: service account email to impersonate through IAM Credentials
      --insecure-storage            Store the login in a 0600 plaintext file instead of the OS keychain
      --no-browser                  Headless login: approve on any device, then paste the redirected URL
      --no-verify                   Skip listing properties after login (no API call)
      --scope string                Search Console access to request: full or readonly (default "full")
      --service-account string      Save a service account JSON key file for this profile
      --subject string              With --service-account: Workspace user to impersonate (domain-wide delegation)
```

## gsc auth logout

Remove a profile and its saved login from this machine

Deletes the profile and its stored token locally. --revoke also revokes access at Google, which
signs this Google account out of every machine and profile that uses the same OAuth client project;
it asks for confirmation unless --yes is given.

```text
gsc auth logout [flags]
```

```bash
  gsc auth logout
  gsc auth logout --profile work --revoke --yes
```

```text
      --revoke   Also revoke access at Google (signs out every machine using the same OAuth client)
```

## gsc auth status

Show which credential is in use, without revealing it

Reads local configuration only. --verify additionally lists properties with the credential (one API call).

```text
gsc auth status [flags]
```

```bash
  gsc auth status
  gsc auth status --verify -o json
```

```text
      --verify   Check the credential by listing properties (one API call)
```

## gsc auth token

Print a fresh access token for debugging (keep it secret)

```text
gsc auth token
```

```bash
  curl -H "Authorization: Bearer $(gsc auth token)" https://searchconsole.googleapis.com/webmasters/v3/sites
  gsc auth token --profile ci
```

## gsc auth use

Set the default profile

```text
gsc auth use NAME
```

```bash
  gsc auth use work
```

## gsc completion

Generate shell completion script

```text
gsc completion [bash|zsh|fish|powershell]
```

```bash
  source <(gsc completion zsh)
  gsc completion bash > /usr/local/etc/bash_completion.d/gsc
  gsc completion fish > ~/.config/fish/completions/gsc.fish
```

## gsc config

Inspect configuration and select profiles

```text
gsc config
```

## gsc config list-profiles

List saved profiles (no secrets)

```text
gsc config list-profiles
```

```bash
  gsc config list-profiles
  gsc config list-profiles -o json
```

## gsc config show

Show which credential is in use, without revealing it

Reads local configuration only. --verify additionally lists properties with the credential (one API call).

```text
gsc config show [flags]
```

```bash
  gsc config show
  gsc config show --verify -o json
```

```text
      --verify   Check the credential by listing properties (one API call)
```

## gsc config use-profile

Set the default profile

```text
gsc config use-profile NAME
```

```bash
  gsc config use-profile work
```

## gsc doctor

Check configuration, credentials, and PATH (local unless --online)

```text
gsc doctor [flags]
```

```bash
  gsc doctor
  gsc doctor --online -o json
```

```text
      --online   Also call the API once to list properties
```

## gsc export

Export Search Analytics day by day to CSV or NDJSON files (resumable)

Splits the range into days, as Google's extraction guide recommends, fetches every exposed row for each
day, and writes one file per day through a temp file and rename. manifest.json records finished days;
rerun the same command to resume. Days after the latest final date, and days Google marks incomplete,
are recorded as preliminary and re-fetched on resume.
No automatic retries unless --retry is given.

```text
gsc export [flags]
```

```bash
  gsc export --last 16m -d query -d page --out ./gsc-export
  gsc export --start 2026-01-01 --end 2026-03-31 -d page --format ndjson --out ./q1
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --data-state string    final (default), all (includes preliminary data), or hourly_all
  -d, --dimension strings    Group by: date, hour, query, page, country, device, searchAppearance (repeat or comma list)
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --format string        File format: csv or ndjson (default "csv")
      --interval duration    Minimum time between the start of one day and the next (a day's pages are fetched back to back) (default 250ms)
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --out string           Output directory (created if missing)
      --restart              Discard a manifest written for a different request
      --retry int            Retry quota and server errors this many times per day, with backoff (default 0)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc freshness

Latest dates with final and with preliminary data

Two small date-grouped queries over the last 10 days: one for final data and one including preliminary
data. Final data usually lags two to three days.

```text
gsc freshness [flags]
```

```bash
  gsc freshness
  gsc freshness --type discover -o json
```

```text
      --type string   Search type (default web)
```

## gsc insights

Opportunities computed locally from explicit queries (thresholds are printed)

Each insight runs ordinary Search Analytics queries and computes the result locally. The thresholds used
are printed on stderr and included in JSON output. Results reflect rows Google exposes; anonymized queries
are never included.

```text
gsc insights
```

## gsc insights cannibalization

Queries where two or more pages split the impressions

```text
gsc insights cannibalization [flags]
```

```bash
  gsc insights cannibalization --last 3m
  gsc insights cannibalization --min-share 0.2 --filter 'query ~ pricing'
```

```text
      --aggregation string      auto, byPage, byProperty, or byNewsShowcasePanel
      --data-state string       final (default), all (includes preliminary data), or hourly_all
      --end string              End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray      Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string             Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int               Maximum queries to show (default 25)
      --min-impressions float   Minimum total impressions for a query (default 100)
      --min-share float         Minimum share of a query's impressions for a page to count as competing (default 0.1)
      --start string            Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string             Search type: web (default), image, video, news, discover, googleNews
```

## gsc insights decliners

Queries or pages that lost the most clicks versus the comparison period

```text
gsc insights decliners [flags]
```

```bash
  gsc insights decliners --compare yoy
  gsc insights decliners --by page --min-clicks 20 -o json
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --by string            Compare queries or pages (default "query")
      --compare string       Comparison period: previous or yoy (default "previous")
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int            Maximum rows to show (default 50)
      --min-clicks float     Minimum clicks in the comparison period (default 5)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc insights lost-queries

Queries or pages returned in the comparison period but not now (no longer exposed)

```text
gsc insights lost-queries [flags]
```

```bash
  gsc insights lost-queries --last 7d
  gsc insights lost-queries --by page -o json
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --by string            Compare queries or pages (default "query")
      --compare string       Comparison period: previous or yoy (default "previous")
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int            Maximum rows to show (default 50)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc insights low-ctr

Queries whose CTR is well below this site's median for the same position

```text
gsc insights low-ctr [flags]
```

```bash
  gsc insights low-ctr
  gsc insights low-ctr --factor 0.4 --min-impressions 200 -o json
```

```text
      --aggregation string      auto, byPage, byProperty, or byNewsShowcasePanel
      --data-state string       final (default), all (includes preliminary data), or hourly_all
      --end string              End date YYYY-MM-DD (Pacific Time, inclusive)
      --factor float            Flag queries with CTR below this fraction of the bucket median (default 0.5)
      --filter stringArray      Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string             Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int               Maximum rows to show (default 50)
      --min-impressions float   Minimum impressions for a query to count (default 100)
      --start string            Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string             Search type: web (default), image, video, news, discover, googleNews
```

## gsc insights new-queries

Queries or pages returned now but not in the comparison period (newly exposed)

```text
gsc insights new-queries [flags]
```

```bash
  gsc insights new-queries --last 7d
  gsc insights new-queries --by page --compare yoy
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --by string            Compare queries or pages (default "query")
      --compare string       Comparison period: previous or yoy (default "previous")
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int            Maximum rows to show (default 50)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc insights striking-distance

Queries ranking just off the top (average position 4 to 20) with real impressions

```text
gsc insights striking-distance [flags]
```

```bash
  gsc insights striking-distance
  gsc insights striking-distance --min-impressions 500 --max-position 15 -o json
```

```text
      --aggregation string      auto, byPage, byProperty, or byNewsShowcasePanel
      --data-state string       final (default), all (includes preliminary data), or hourly_all
      --end string              End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray      Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string             Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int               Maximum rows to show (default 50)
      --max-position float      Highest average position to include (default 20)
      --min-impressions float   Minimum impressions in the period (default 100)
      --min-position float      Lowest average position to include (default 4)
      --start string            Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string             Search type: web (default), image, video, news, discover, googleNews
```

## gsc inspect

URL Inspection: Google's indexed view of URLs (not a live test)

Calls the URL Inspection API for each URL. It reports the indexed version only: there is no live test and
no request-indexing in the API. Google allows 2,000 inspections per property per day and 600 per minute;
batches are paced below that and stop cleanly when a quota is reached.

```text
gsc inspect [URL...] [flags]
```

```bash
  gsc inspect https://www.example.com/pricing
  gsc inspect --file urls.txt -o json
  cat urls.txt | gsc inspect --file - --max 200
```

```text
      --file string         Read URLs from a file, one per line (- for stdin)
      --interval duration   Pause between inspections (stays under 600 per minute) (default 120ms)
      --language string     BCP 47 language for issue messages (default en-US)
      --max int             Inspect at most this many URLs (the per-property daily quota is 2,000) (default 2000)
```

## gsc login

Alias of auth login

With no flags, opens Google's consent page in your browser and saves the login in the OS keychain.
Tokens refresh automatically afterwards. --no-browser prints a URL to approve on any device and
asks you to paste the final redirected URL back. --service-account and --adc save non-interactive
credentials for CI. See docs/auth.md for every method.

```text
gsc login [flags]
```

```bash
  gsc login
  gsc login --no-browser
  gsc login --profile work --scope readonly
```

```text
      --adc                         Use Application Default Credentials (gcloud, workload identity federation, metadata server)
      --client-secret-file string   Use your own Desktop OAuth client (JSON from Google Cloud Console)
      --impersonate string          With --adc: service account email to impersonate through IAM Credentials
      --insecure-storage            Store the login in a 0600 plaintext file instead of the OS keychain
      --no-browser                  Headless login: approve on any device, then paste the redirected URL
      --no-verify                   Skip listing properties after login (no API call)
      --scope string                Search Console access to request: full or readonly (default "full")
      --service-account string      Save a service account JSON key file for this profile
      --subject string              With --service-account: Workspace user to impersonate (domain-wide delegation)
```

## gsc performance

Totals for a period: clicks, impressions, CTR, average position

A query with no dimensions, so totals match the Performance report chart (anonymized queries included).

```text
gsc performance [flags]
```

```bash
  gsc performance --last 28d --compare previous
  gsc performance --type discover --last 3m -o json
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --compare string       Also query a comparison period: previous or yoy
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc query

Search Analytics with every API option (dimensions, filters, types, data state)

Runs searchAnalytics.query. Without dates it covers the last 28 days ending at the latest day with data
(one small extra request finds that day; pass --end to skip it). Results are sorted by clicks by Google.
"complete" in the output means every row Google exposes was fetched; anonymized queries are never included.

```text
gsc query [flags]
```

```bash
  gsc query -s sc-domain:example.com -d query -d page --limit 5000 -o csv
  gsc query --last 3m -d date --filter 'country = ind' --filter 'query ~ shoes'
  gsc query -d hour --data-state hourly_all --last 2d -o json
  gsc query -d query --compare previous --limit 500
```

```text
      --aggregation string    auto, byPage, byProperty, or byNewsShowcasePanel
      --all                   Fetch every row Google exposes for the query
      --compare string        Also query a comparison period: previous or yoy
      --data-state string     final (default), all (includes preliminary data), or hourly_all
  -d, --dimension strings     Group by: date, hour, query, page, country, device, searchAppearance (repeat or comma list)
      --end string            End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray    Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string           Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int             Maximum rows to return (pages of up to 25,000 are fetched) (default 1000)
      --print-request         Print the request body and send nothing
      --raw                   Print the API response unchanged (single page, no --all or --compare)
      --request-file string   JSON request body to start from (flags override its fields; - for stdin)
      --start string          Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string           Search type: web (default), image, video, news, discover, googleNews
```

## gsc sitemaps

List, inspect, submit, and delete sitemaps for a property

```text
gsc sitemaps
```

## gsc sitemaps delete

Delete a sitemap from the report (Google may still crawl it)

```text
gsc sitemaps delete SITEMAP_URL
```

```bash
  gsc sitemaps delete https://www.example.com/old-sitemap.xml --yes
```

## gsc sitemaps get

Show one sitemap's status, errors, and warnings

```text
gsc sitemaps get SITEMAP_URL
```

```bash
  gsc sitemaps get https://www.example.com/sitemap.xml
```

## gsc sitemaps list

List submitted sitemaps

```text
gsc sitemaps list [flags]
```

```bash
  gsc sitemaps list
  gsc sitemaps list --index https://www.example.com/sitemap_index.xml -o json
```

```text
      --index string   Only sitemaps listed in this sitemap index URL
```

## gsc sitemaps submit

Submit a sitemap URL for the property

```text
gsc sitemaps submit SITEMAP_URL
```

```bash
  gsc sitemaps submit https://www.example.com/sitemap.xml
  gsc sitemaps submit https://www.example.com/sitemap.xml --dry-run
```

## gsc sites

List and manage Search Console properties

```text
gsc sites
```

## gsc sites add

Add a property to your Search Console list (does not verify ownership)

SITE must be exact: sc-domain:example.com or a URL-prefix such as https://www.example.com/.
Adding a property does not verify it. A URL-prefix inside a domain property you already own is verified
at once; otherwise complete verification in Search Console. The output reports the resulting permission.

```text
gsc sites add SITE
```

```bash
  gsc sites add https://blog.example.com/
  gsc sites add sc-domain:example.org --dry-run
```

## gsc sites get

Show one property (default: the profile's site)

```text
gsc sites get [SITE]
```

```bash
  gsc sites get
  gsc sites get https://www.example.com/ -o json
```

## gsc sites list

List properties you can access and your permission level

```text
gsc sites list
```

```bash
  gsc sites list
  gsc sites list -o json
```

## gsc sites remove

Remove a property from your Search Console list

```text
gsc sites remove SITE
```

```bash
  gsc sites remove https://old.example.com/ --yes
```

## gsc sites use

Set the profile's default site

Exact property identifiers are saved without a network call; a bare host is resolved with one sites.list call.

```text
gsc sites use SITE
```

```bash
  gsc sites use sc-domain:example.com
  gsc sites use www.example.com
```

## gsc status

Alias of auth status

Reads local configuration only. --verify additionally lists properties with the credential (one API call).

```text
gsc status [flags]
```

```bash
  gsc status
  gsc status --verify
```

```text
      --verify   Check the credential by listing properties (one API call)
```

## gsc top

Top queries, pages, countries, devices, or search appearances

```text
gsc top
```

## gsc top appearance

Search appearance types (filter on one with --filter 'searchAppearance = VALUE' in gsc query)

```text
gsc top appearance [flags]
```

```bash
  gsc top appearance --last 3m -o json
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --all                  Fetch every row Google exposes for the query
      --compare string       Also query a comparison period: previous or yoy
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int            Maximum rows to return (pages of up to 25,000 are fetched) (default 25)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc top countries

Clicks and impressions by country (ISO 3166-1 alpha-3)

```text
gsc top countries [flags]
```

```bash
  gsc top countries --last 3m
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --all                  Fetch every row Google exposes for the query
      --compare string       Also query a comparison period: previous or yoy
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int            Maximum rows to return (pages of up to 25,000 are fetched) (default 25)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc top devices

Clicks and impressions by device

```text
gsc top devices [flags]
```

```bash
  gsc top devices --compare yoy
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --all                  Fetch every row Google exposes for the query
      --compare string       Also query a comparison period: previous or yoy
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int            Maximum rows to return (pages of up to 25,000 are fetched) (default 25)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc top pages

Top pages by clicks

```text
gsc top pages [flags]
```

```bash
  gsc top pages --filter 'page ~ /blog/'
  gsc top pages --all -o csv > pages.csv
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --all                  Fetch every row Google exposes for the query
      --compare string       Also query a comparison period: previous or yoy
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int            Maximum rows to return (pages of up to 25,000 are fetched) (default 25)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc top queries

Top search queries by clicks

```text
gsc top queries [flags]
```

```bash
  gsc top queries --last 7d
  gsc top queries --compare previous --limit 50 -o json
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --all                  Fetch every row Google exposes for the query
      --compare string       Also query a comparison period: previous or yoy
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --limit int            Maximum rows to return (pages of up to 25,000 are fetched) (default 25)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc trend

Clicks, impressions, CTR, and position over time

Groups by date (default), hour (last 10 days, preliminary data), ISO week, or calendar month.
Week and month totals are rolled up locally from daily rows: clicks and impressions summed; CTR recomputed from the sums; position weighted by impressions.

```text
gsc trend [flags]
```

```bash
  gsc trend --last 3m --by week
  gsc trend --by hour --last 2d
  gsc trend --last 16m --by month --filter 'page ~ /blog/'
```

```text
      --aggregation string   auto, byPage, byProperty, or byNewsShowcasePanel
      --by string            Granularity: date, hour, week, or month (default "date")
      --data-state string    final (default), all (includes preliminary data), or hourly_all
      --end string           End date YYYY-MM-DD (Pacific Time, inclusive)
      --filter stringArray   Filter 'DIM OP VALUE'; OP is = != ~ !~ =~ !=~ (contains/regex); repeat to AND
      --last string          Window ending at the latest available date: 7d, 28d, 4w, 3m, 16m (default 28d)
      --start string         Start date YYYY-MM-DD (Pacific Time, inclusive)
      --type string          Search type: web (default), image, video, news, discover, googleNews
```

## gsc update

Update gsc to the latest release (SHA-256 verified)

Downloads the latest GitHub release for this OS and architecture, verifies it against the release's
checksums.txt, and replaces the running gsc executable. Works on macOS, Linux, and Windows; on Windows
the old executable is renamed to gsc.exe.old and deleted on a later run.

Asks "Update now? [Y/n]" when stdin is a terminal; --yes skips the question, and --no-input requires
--yes. --check only reports the current and latest versions. A gsc in a Go bin directory ($GOBIN,
$GOPATH/bin, or ~/go/bin) is not replaced: rebuild it from your checkout instead. --read-only blocks
installing but allows --check.

Update notice: in an interactive terminal, gsc checks the github.com releases page (not the GitHub API,
so its rate limit never applies) for a new release at most once a day and, after a command's output,
prints a notice on stderr. The command that runs the day's check waits up to 1 second after its output
for the answer; other commands never wait. gsc update and update --check store their result in the same
cache. It never checks when stderr is not a terminal, when CI is set, with --quiet, or when
GSC_NO_UPDATE_NOTIFIER or NO_UPDATE_NOTIFIER is set (to anything).

```text
gsc update [flags]
```

```bash
  gsc update --check
  gsc update
  gsc update --yes --no-input
```

```text
      --check   Only report the current and latest versions (always checks the github.com releases page)
```

## gsc version

Print build information

Prints the version, commit, build date, and whether a built-in OAuth client is present. latest and
update_available come from the last release check (see gsc update --help) and appear only when one is
cached; version never uses the network.

```text
gsc version
```

```bash
  gsc version
  gsc version -o json
```
