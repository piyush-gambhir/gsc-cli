# gsc-cli: architecture and build plan

A Go command-line interface for Google Search Console: search performance analytics, URL inspection,
sitemaps, and properties. Built for people and coding agents, matching the sibling CLIs in this folder.
Facts, limits, and sources are in [RESEARCH.md](RESEARCH.md); this file is the design.

Status (2026-10-03): P0 to P5 built in `cli-go/`, with docs, a vendored API snapshot, and a coverage test.
The code passed three independent review rounds (gpt-6.1-sol); every confirmed finding was fixed with tests.
Remaining owner work: create the built-in OAuth client, run one real login and a live smoke test, and P6
(public launch and Google verification). Recorded choices are in [Decisions](#decisions).

## Goals

1. **Seamless login first.** `gsc auth login` opens a browser, the user clicks Allow once, and every
   command works from then on. No Google Cloud project, no JSON key files, no token copying. Advanced
   credential types come in a later phase.
2. **Full API coverage.** All ten active Search Console methods: four sites, four sitemaps, Search
   Analytics, and URL Inspection.
3. **Better analytics than the web UI**: no 1,000-row table cap, every query option including hourly data,
   period comparison, resumable bulk export, and computed insights.
4. **Agent-safe and predictable**: `--read-only` mode, a reviewed command-safety manifest, stable JSON,
   data on stdout and diagnostics on stderr, no hidden network calls or automatic retries.

Not in scope: anything the API does not expose. That includes Core Web Vitals, the Page Indexing report,
manual actions, links, live URL tests, "request indexing", and the 2025-2026 UI-only features (branded
query filter, query groups, generative-AI reports, multimodal reporting). Platform properties (Instagram,
TikTok, X, YouTube, announced July 2026) work only as far as `sites list` returns them.

## Identity

| Item | Value |
| --- | --- |
| Repository | `github.com/piyush-gambhir/gsc-cli` (local folder `gsc-cli/`) |
| Go module | `github.com/piyush-gambhir/gsc-cli/cli-go` |
| Binary | `gsc` ([decision 1](#decisions)) |
| Env prefix | `GSC_` |
| Config | `~/.config/gsc-cli/config.yaml` (XDG-aware, 0600, flock + atomic write); never holds secrets unless `--insecure-storage` was chosen |
| Secrets | OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service) |
| Go | 1.26 minimum, toolchain 1.27.1 (suite baseline) |
| Layout | `cli-go/`, `docs/`, `gsc/SKILL.md`, `.github/`, `install.sh`, root `Makefile` (same as `clarity-cli`) |

## Authentication

### Phase 1: one-command browser login

```console
$ gsc auth login
Opening your browser to sign in with Google...
Logged in as me@example.com (profile "default").
Found 3 properties. Default site set to sc-domain:example.com (change with: gsc sites use SITE).
```

What happens:

1. The CLI starts a listener on `127.0.0.1` with an OS-assigned port (Desktop OAuth clients accept any
   loopback port, so nothing can collide).
2. It opens the browser at Google's consent page using the **built-in OAuth client**, with PKCE (S256),
   a random `state`, `access_type=offline`, and `prompt=consent` so a refresh token is always returned.
   The URL is also printed to stderr in case the browser does not open.
3. The user clicks Allow. The CLI validates `state`, exchanges the code, and reads the account email
   from the ID token.
4. The refresh token goes into the OS keychain. Google's OAuth policy requires tokens to be encrypted at
   rest, so there is no silent plaintext fallback: on a machine without a keychain service (a bare Linux
   server, some WSL setups) login stops and explains the choice, which is `--insecure-storage` (a 0600
   file, recorded on the profile) or `GSC_ACCESS_TOKEN`. Keychain calls time out after 10 seconds, so a
   locked keychain waiting on an unlock prompt fails cleanly instead of hanging.
5. The CLI calls `sites list` once. With one property it becomes the default site; with several, an
   interactive picker appears (skipped under `--no-input`, where the first domain property is chosen and
   reported).

After that, access tokens refresh silently (cached until 60 seconds before expiry), so users never see
expiry. If Google revokes the grant (`invalid_grant`), the error says exactly what to run:
`gsc auth login --profile NAME`.

Scope: the full `webmasters` scope, so one consent covers every command, including sitemap submission.
The CLI's own `--read-only` flag protects agents from writes. See [decision 3](#decisions) for
switching the default to `webmasters.readonly` before a public launch.

Also in phase 1, because they cost almost nothing:

- `GSC_ACCESS_TOKEN` / `--access-token`: use a token from anywhere, for example
  `gcloud auth print-access-token`. No refresh, nothing stored.
- `gsc auth status` (local, no network; `--verify` makes one call), `auth list`, `auth use NAME`,
  `auth token` (prints a fresh access token for `curl` debugging), and `auth logout`.
- `auth logout` deletes the stored token locally. `--revoke` also revokes access at Google. It is
  opt-in and asks for confirmation (or `--yes`) because Google revokes the user's tokens for every client
  in the project, signing out every other machine and profile that uses the built-in client.
- Each profile records the OAuth client ID it logged in with (built-in or overridden), so a refresh
  always uses the same client; if that client's secret is no longer available, the error asks for a
  fresh login instead of failing obscurely.
- Refreshes take a per-profile cross-process lock and re-read the stored token after acquiring it, so
  concurrent commands do not race each other or resurrect a profile that was just logged out.

### Built-in OAuth client: one-time owner setup

The seamless flow needs an OAuth client that ships with the binary. Google's native-app model treats
Desktop client secrets as non-confidential (PKCE protects the flow), which is how gcloud ships its client.
Google's policy forbids committing client credentials to public repositories, so the values are injected
at build time, never stored in the source tree.

1. Create a Google Cloud project (for example `gsc-cli`) and enable the **Google Search Console API**.
2. Configure the OAuth consent screen: **External**, app name and support email, then set publishing
   status to **In production**. Do not stay in Testing, because Testing refresh tokens expire after 7 days.
3. Create an OAuth client of type **Desktop app**.
4. Store the client ID and secret as GitHub Actions secrets for release builds, and in an untracked
   `cli-go/.env.local` for local builds. A build without them falls back to an error explaining how to
   provide `GSC_CLIENT_ID` and `GSC_CLIENT_SECRET`.

Until the app is verified, the consent page shows Google's "unverified app" notice (users click
Advanced, then continue), and the client is capped at 100 users in total. That is fine for personal and
team use. Verification is part of the later auth phase.

Project quotas are shared by everyone using the built-in client. For one person or a team this is far
below the limits (Search Analytics allows 40,000 queries per minute per project).

### Later: auth expansion (phase 5)

| Method | Command | For |
| --- | --- | --- |
| Headless login | `gsc auth login --no-browser` | SSH sessions: approve on any device, paste the final redirect URL back. Same loopback redirect and PKCE, not Google's retired out-of-band flow |
| Bring your own OAuth client | `gsc auth login --client-secret-file client_secret.json` | Teams that want their own quota or branding |
| Service account | `gsc auth login --service-account key.json [--subject user@domain]` | CI and servers; add the service account email as a user on each property |
| Application Default Credentials | `gsc auth login --adc` | gcloud users (with `--client-id-file`, since gcloud's default client is not meant for non-Cloud scopes), GitHub Actions Workload Identity Federation, GCE and Cloud Run |
| Impersonation | `--impersonate sa@project.iam.gserviceaccount.com` | Keyless CI through the IAM Credentials API |
| Read-only scope | `gsc auth login --scope readonly` | Least privilege |
| Google verification | owner task | Removes the warning and the 100-user cap for a public release |

Credential precedence once all exist: `--access-token` / `GSC_ACCESS_TOKEN` > `--credentials FILE` /
`GSC_CREDENTIALS` > selected profile. Profile selection: `--profile` > `GSC_PROFILE` > saved current. There
is never a silent fallback to ambient ADC, so an agent never runs as an identity nobody chose.

Not possible: Google's device-code flow (its scope allowlist excludes Search Console) and API keys (the
API requires OAuth).

## Command surface

`(w)` marks a remote write: annotated `mutates=true`, blocked by `--read-only`, supports `--dry-run`, and
destructive ones confirm interactively or need `--yes`. `(l)` changes only local state. Phases refer to
[Milestones](#milestones).

```text
gsc auth login | status | list | use NAME | logout | token                          (l)  P1
gsc login, gsc status             top-level aliases (suite convention)                  P1
gsc config show | list-profiles | use-profile NAME                                 (l)  P1

gsc sites list                                                                          P2
gsc sites get [SITE]                                                                    P2
gsc sites use SITE                set the profile's default site                   (l)  P2
gsc sites add SITE                adds to your list; does not verify ownership     (w)  P3
gsc sites remove SITE                                                              (w)  P3

gsc sitemaps list [--index SITEMAP_URL]                                                 P2
gsc sitemaps get SITEMAP_URL                                                            P2
gsc sitemaps submit SITEMAP_URL                                                    (w)  P3
gsc sitemaps delete SITEMAP_URL   removes the report entry; Google may still crawl (w)  P3

gsc query                         Search Analytics with every API option                P2
gsc performance                   totals: clicks, impressions, CTR, position            P2
gsc top queries|pages|countries|devices|appearance                                     P2
gsc trend [--by date|hour|week|month]                                                   P2
gsc freshness                     latest final and preliminary dates                    P2
gsc inspect URL... [--file F]     URL Inspection (indexed version, not a live test)     P2

gsc export                        day-chunked, resumable export to CSV or NDJSON        P4
gsc insights striking-distance|low-ctr|cannibalization|decliners|new-queries|lost-queries P4
gsc api METHOD PATH [--data JSON|@file]   raw authenticated request                     P4
gsc doctor                        config, credentials, scopes, site access, PATH check  P4

gsc completion | version | update [--check]                                             P1
```

### Site selection

Site-scoped commands take `--site` / `-s`, then `GSC_SITE`, then the profile's default site. Exact
property strings (`sc-domain:example.com`, `https://www.example.com/`) are used as returned by
`sites list`, never normalized. A bare host (`example.com`) is matched against `sites list`: a domain
property wins, otherwise a single URL-prefix match; ambiguity is an error listing the candidates. The
whole identifier is escaped as one path segment.

### `gsc query`

```bash
gsc query -s sc-domain:example.com --last 28d \
  -d query -d page --type web \
  --filter 'country = ind' --filter 'query ~ running shoes' --filter 'page =~ ^https://example.com/blog/' \
  --limit 5000 -o csv
```

| Flag | Meaning |
| --- | --- |
| `--start`, `--end` | inclusive `YYYY-MM-DD` dates in Pacific Time (Search Console's timezone) |
| `--last` | `7d`, `28d`, `3m`, `6m`, `12m`, `16m`; ends at the latest date available for the chosen data state |
| `-d, --dimension` | repeatable or comma list: `date`, `hour`, `query`, `page`, `country`, `device`, `searchAppearance` |
| `--type` | `web` (default), `image`, `video`, `news`, `discover`, `googleNews` |
| `--filter` | `DIM OP VALUE`; ops `=`, `!=`, `~` (contains), `!~`, `=~` (RE2 regex), `!=~`; repeated filters are ANDed (the API has no OR) |
| `--data-state` | `final` (default), `all` (includes preliminary data), `hourly_all` (required for `hour`) |
| `--aggregation` | `auto`, `byPage`, `byProperty`, `byNewsShowcasePanel` |
| `--limit` | rows to return; fetched in pages of up to 25,000 with `startRow` |
| `--all` | page until Google returns an empty page (bounded by a page guard) |
| `--compare previous\|yoy` | a second, explicit query for the comparison period; rows joined on dimension keys with `_prev`, `_delta`, `_pct` columns |
| `--request-file` | a full JSON request body; flags override its fields. `--print-request` shows the body and sends nothing |
| `--raw` | print the API response unchanged |

Validation runs before any network call, limited to restrictions Google documents: date order, `hour`
only with `hourly_all`, duplicate dimensions, `byProperty` with page grouping or filtering or with
`discover`/`googleNews`, `byNewsShowcasePanel` prerequisites, and regex syntax. Retention (16 months,
10 days for hourly data) describes availability, not a request rule: explicit dates beyond it produce a
warning, and helper-generated ranges (`--last 16m`, comparison periods) are clipped with the clipping
reported. Anything else is left to the server, whose error is shown verbatim.

`searchAppearance` cannot be combined with other grouping dimensions, so `gsc top appearance` lists the
appearance values first; filter on one of them to break it down by other dimensions.

Output: table columns are the requested dimensions in order, then `clicks`, `impressions`, `ctr` (shown as
a percentage), `position`. JSON is an envelope so agents get what they need to interpret the numbers:

```json
{
  "site": "sc-domain:example.com",
  "start": "2026-09-03", "end": "2026-09-30",
  "type": "web", "data_state": "final",
  "dimensions": ["query", "page"],
  "aggregation": "byPage",
  "first_incomplete_date": null, "first_incomplete_hour": null, "metadata_returned": false,
  "row_count": 5000, "complete": false,
  "rows": [{"query": "running shoes", "page": "https://example.com/shoes", "clicks": 120, "impressions": 4810, "ctr": 0.0249, "position": 7.42}]
}
```

`ctr` stays a ratio in JSON, YAML, and CSV. `complete` is about pagination only: it is true when Google
returned an empty page, which means "all exposed rows", not every query (anonymized queries and the
50,000-rows-per-day cap still apply). Data finality is separate: Google returns incompleteness metadata
only for `all` with date grouping or `hourly_all` with hour grouping, so `metadata_returned` says
whether the `first_incomplete_*` fields are known or simply not provided. Both camelCase and snake_case
spellings are decoded, because Google's docs disagree.

`--compare` rules: each period reports its own row count and completeness. A key missing from one
period is `null` (unobserved), never zero, because truncated results can omit rows that exist. Percentage
change is `null` when the earlier value is zero, CTR changes are reported in percentage points, and
position changes as absolute differences.

### Analytics helpers

- `performance`: no dimensions, so totals match the UI's chart totals. Supports `--compare`.
- `top queries|pages|countries|devices|appearance`: single-dimension queries, `--limit 25` by default.
- `trend`: `date` or `hour` rows. `--by week|month` rolls days up locally: clicks and impressions are
  summed, CTR is recomputed from the sums, and position is impression-weighted. The method is stated in the
  output.
- `freshness`: two small date-grouped queries (`final` and `all`) reporting the latest complete and latest
  preliminary dates. Data is typically final after two to three days.
- `export` (P4): splits the range into days, as Google's extraction guide recommends, pages through each
  day, and writes one file per day (CSV or NDJSON) through a temp file and atomic rename, so an
  interrupted day leaves nothing behind. A manifest bound to the site, request, dimensions, data state,
  and output schema records committed days; rerunning resumes, and a changed request starts fresh. Days
  that were still preliminary are re-fetched on resume, and `--data-state final` is the default.
  Requests are paced under the per-site limit. No automatic retries by default; `--retry N` opts in, and
  every retry is logged on stderr.
- `insights` (P4): computed locally from explicit queries. Each prints its thresholds and accepts flags to
  change them.
  - `striking-distance`: queries at average position 4 to 20 with high impressions.
  - `low-ctr`: CTR well below the site's own median for the same position bucket.
  - `cannibalization`: queries where two or more pages share meaningful impressions.
  - `decliners`, `new-queries`, `lost-queries`: period comparisons. "New" and "lost" mean newly
    exposed or no longer exposed rows within the fetched limit, and the output says so.

### `gsc inspect`

URL Inspection for one URL or many (`--file`, or `-` for stdin). The table shows verdict, coverage state,
indexing state, last crawl, page fetch state, robots.txt state, and Google-selected versus user-declared
canonical; JSON returns the full result including rich results. Batch mode paces itself under 600 per
minute per site, stops cleanly at the 2,000-per-day site quota, and `--max N` caps spend. It reports the
indexed version only: no live test, no request indexing, because the API offers neither.

### `gsc api`

`gsc api GET /webmasters/v3/sites` sends an authenticated request to `searchconsole.googleapis.com`. GET
and the two read-only POSTs (`searchAnalytics/query`, `urlInspection/index:inspect`) count as reads;
anything else is a write for `--read-only`. This keeps the CLI useful the day Google ships a new method.

## Global flags and environment

| Flag | Env | Meaning |
| --- | --- | --- |
| `-o, --output` | | `table` (default), `json`, `yaml`, `csv` |
| `--profile` | `GSC_PROFILE` | named profile |
| `-s, --site` | `GSC_SITE` | property for site-scoped commands |
| `--access-token` | `GSC_ACCESS_TOKEN` | raw bearer token override |
| `--timeout` | | per-request timeout, default 30s |
| `--no-input` | `GSC_NO_INPUT` | never prompt or open a browser |
| `-q, --quiet` | `GSC_QUIET` | suppress informational stderr |
| `-v, --verbose` | `GSC_VERBOSE` | log method, sanitized URL, and status (never tokens, codes, or bodies) |
| `--read-only` | `GSC_READ_ONLY` | block remote writes, local credential changes, and self-update |
| `--dry-run` | | for `(w)` commands: print the request and exit |
| `--yes` | | confirm destructive commands non-interactively |
| | `GSC_CONFIG`, `XDG_CONFIG_HOME` | file locations |
| | `GSC_CLIENT_ID`, `GSC_CLIENT_SECRET` | override the built-in OAuth client |

`-o csv` extends the suite's three formats because Search Console data usually ends up in a spreadsheet.
It applies to row-shaped results; other commands reject it with a clear message.

Config (phase 1 shape; secrets live in the keychain):

```yaml
current_profile: default
profiles:
  default:
    auth: oauth
    account: me@example.com
    client: builtin
    scopes: [openid, email, https://www.googleapis.com/auth/webmasters]
    token_store: keychain      # "file" only after an explicit --insecure-storage
    site: sc-domain:example.com
```

## Errors and quotas

- Google's error envelope (`error.code`, `error.message`, `error.errors[].reason`, and the newer
  `error.status` / `error.details`) becomes a structured error with HTTP status, reason, and Retry-After
  when present. JSON and YAML modes print it structurally on stderr, as clarity-cli does.
- Quota failures can arrive as 403 or 429, so messages key off the reason. Search Analytics also has
  unpublished "load" quotas: page plus query grouping over long ranges is expensive. The message suggests
  narrowing the range or dimensions and waiting 15 minutes, as Google advises.
- No automatic retries anywhere by default. Batch commands resume instead.
- Redirects never forward credentials, response bodies are size-capped, and tokens, codes, and secrets are
  redacted from every error and log line.

## Architecture

```text
cli-go/
  main.go
  cmd/                 root, auth, config, sites, sitemaps, query, analytics (performance/top/trend/
                       freshness), inspect, export, insights, api, doctor, update; one file per noun
  internal/auth/       loopback OAuth with PKCE, built-in client (ldflags), token refresh and cache,
                       revoke; later: paste flow, service account, ADC, impersonation
  internal/secrets/    keychain store (10s call timeout) with opt-in 0600 file storage
  internal/client/     HTTP core, Google error envelope, typed calls for the ten methods, raw requests
  internal/analytics/  filter DSL, Pacific-time date ranges, pagination, compare join, rollups, insights
  internal/site/       property resolution and escaping
  internal/export/     chunk planner, resume manifest, CSV and NDJSON writers
  internal/config/     profiles (clarity-cli pattern, extended schema)
  internal/output/     table, json, yaml, csv
  internal/build/, internal/update/   from clarity-cli
  tools/gendocs/
```

Dependencies: the suite set (cobra, flock, renameio, yaml, x/term, x/sys) plus `golang.org/x/oauth2`
(v0.37.0: PKCE helpers, token sources, and later Google credential types) and `github.com/zalando/go-keyring`
for the keychain. The generated `google.golang.org/api/searchconsole/v1` client is not used: ten methods
do not justify its dependency graph, and hand-written types keep JSON shapes under our control.

## Safety model

- `--read-only` blocks every `(w)` command, local credential changes, and self-update. Reads that use POST
  (`query`, `inspect`) stay allowed: classification is by effect, not HTTP method.
- Destructive commands (`sites remove`, `sitemaps delete`) confirm interactively or need `--yes`.
- A command-safety manifest test (from jira-cli) hashes every runnable command with its `mutates` and
  `interactive` annotations, so adding or reclassifying a command needs a deliberate hash update.
- `--no-input` never opens a browser or waits for a callback.
- `doctor` warns when another `gsc` binary is on `PATH`.

## Testing

- Fake HTTP transports only; no live calls in CI. A manual smoke script behind `GSC_LIVE=1` checks a real
  property after P1 and P2.
- OAuth: an `httptest` authorization server and a simulated browser hit on the loopback URL cover PKCE,
  state mismatch, denial, timeout, refresh, refresh responses without a new refresh token,
  `invalid_grant`, and revoke. Keychain: a mock store, the timeout path, and the explicit file opt-in.
- Request bodies: golden tests for `query` flag combinations; filter DSL table tests; Pacific-time edge
  cases (DST changes, month ends); pagination termination and page guard; compare joins with missing keys;
  rollup math; export resume after an injected failure.
- Escaping of URL-prefix, domain, sitemap, and percent-containing identifiers.
- Output: table/json/yaml/csv per command shape, Unicode, broken writers. Suite CI: gofmt, vet, race
  tests, generated docs check, install smoke test on macOS, Linux, and Windows, GoReleaser snapshot.

## Docs, skill, release

- `README.md`, `CLAUDE.md`, `CONTRIBUTING.md`, `SECURITY.md`, generated `docs/commands.md`, `docs/auth.md`,
  and `gsc/SKILL.md` (agent guidance: use `-o json`, check `freshness` before trusting recent days, never
  present `all` data as final, never sum query rows as site totals, respect inspection quotas).
- CI, CodeQL, Dependabot, GoReleaser, and `install.sh` copied from clarity-cli. Release builds inject the
  built-in OAuth client from repository secrets.

## Milestones

| Phase | Scope | Done when |
| --- | --- | --- |
| P0 | Scaffold from clarity-cli: root, output (plus csv), config, build, update, gendocs, CI, release, install | `make test vet build docs` pass; `gsc version` runs |
| P1 | Seamless login: built-in OAuth browser flow, keychain storage (explicit `--insecure-storage` opt-in), silent refresh, auto default site, profiles, logout (`--revoke` opt-in), `GSC_ACCESS_TOKEN` | Fake-server tests pass; one real login on macOS works end to end |
| P2 | Reads: sites, sitemaps list/get, `query` with every option, performance/top/trend/freshness, `inspect` | Golden request tests; a real property's `performance` matches the UI totals |
| P3 | Writes: sites add/remove, sitemaps submit/delete, `--dry-run`, confirmations, safety manifest | Read-only paths tested; **v0.1.0** as a personal/team release (unverified OAuth app) |
| P4 | Power: `--compare`, `export`, `insights`, `api`, `doctor` | Export resumes after interruption; insight thresholds documented; **v0.2.0** |
| P5 | Auth expansion: headless paste flow, BYO client, service accounts, ADC and federation, impersonation, read-only scope option | Each method tested against fakes and smoke-tested once; **v0.3.0** |
| P6 | Public launch: homepage and privacy policy (for example at `projects.piyushgambhir.com/gsc-cli`), Google branding and scope verification, then announce | Verified consent screen; public release |

## Decisions

1. **Binary name: `gsc`.** It collides with the `gsc` binary from Homebrew's `ghostscript`,
   `gambit-scheme`, and `gerbil-scheme` formulae and Debian's Gambit package. `install.sh` warns when another
   `gsc` is earlier on `PATH`, and `gsc doctor` reports any other `gsc` on `PATH`.
2. **Built-in OAuth client: yes**, injected at build time (`internal/auth.BuiltinClientID/Secret` via
   `-ldflags`, from `GSC_OAUTH_CLIENT_ID`/`GSC_OAUTH_CLIENT_SECRET`). Empty in source; `GSC_CLIENT_ID` and
   `GSC_CLIENT_SECRET` override it. The owner still has to create the Desktop client (docs/auth.md).
3. **Default scope: full `webmasters`.** `--scope readonly` is available. Before a public launch, check
   Cloud Console: if `webmasters.readonly` is non-sensitive there, defaulting to it avoids the
   sensitive-scope review for read-only users.
4. **Indexing API: skipped**, recorded in docs/api-coverage.md with the reason. Revisit only for a site with
   job-posting or livestream pages, adding a structured-data eligibility check.
5. **CSV output: yes**, for row-shaped results, with spreadsheet formula-injection protection.

Built differently from the plan, with reasons:

- **`export` is allowed under `--read-only`.** It only writes the data files the user asked for, like
  redirecting `-o csv`; it changes no credentials and no remote state.
- **Export manifest mismatch needs `--restart`** instead of silently starting fresh, so a changed request
  never overwrites earlier output by accident.
- **Service account keys are referenced, not embedded.** Profiles store the key path. Embedding (`--embed`)
  was dropped because Windows Credential Manager caps secrets at 2,560 bytes, smaller than a key file.
- **`--last` probes the latest available date** with one small query (documented in `gsc query --help`);
  `--end` skips the probe. Month windows anchored on a month's last day cover whole calendar months.
- **Keychain calls are serialized per process** in addition to the 10-second timeout, because some backends
  (and go-keyring's test mock) are not safe for concurrent use.
