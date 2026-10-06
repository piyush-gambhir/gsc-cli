# gsc: Google Search Console CLI

A Go command-line interface for Google Search Console: search performance, URL inspection, sitemaps, and
properties. One-command browser login, every Search Analytics option, period comparisons, resumable bulk
export, and computed insights.

Designed for people and coding agents: named profiles, table/JSON/YAML/CSV output, `--read-only` mode, data
on stdout and diagnostics on stderr, and a single cross-platform binary. Independent project; not affiliated
with Google.

**Docs:** [projects.piyushgambhir.com/gsc-cli](https://projects.piyushgambhir.com/gsc-cli)
([llms.txt](https://projects.piyushgambhir.com/gsc-cli/llms.txt) for agents)

[![CI](https://github.com/piyush-gambhir/gsc-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/piyush-gambhir/gsc-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/piyush-gambhir/gsc-cli)](https://github.com/piyush-gambhir/gsc-cli/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/piyush-gambhir/gsc-cli/badge)](https://scorecard.dev/viewer/?uri=github.com/piyush-gambhir/gsc-cli)

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/piyush-gambhir/gsc-cli/main/install.sh | sh
```

Installs to `~/.local/bin` (override with `INSTALL_DIR`; pin with `VERSION=v0.1.5`) after verifying the
SHA-256 checksum. Windows users download the ZIP from the [releases page](https://github.com/piyush-gambhir/gsc-cli/releases).

Ghostscript and Gambit Scheme also install a program named `gsc`. The installer warns if another `gsc` comes
first on your `PATH`, and `gsc doctor` checks it too.

From source (Go 1.26+, toolchain 1.27.1):

```bash
git clone https://github.com/piyush-gambhir/gsc-cli.git
cd gsc-cli/cli-go
make build            # bin/gsc
make install          # $GOBIN or $(go env GOPATH)/bin, or INSTALL_DIR=...
```

Source builds have no built-in OAuth client unless you provide one (see [docs/auth.md](docs/auth.md)).

### Update

```bash
gsc update --check    # current and latest version; -o json for scripts
gsc update            # asks, then installs the latest release after SHA-256 verification
```

`gsc update` works on macOS, Linux, and Windows. It downloads the release archive for your platform, checks
it against the release's `checksums.txt`, and replaces the running executable (on Windows the old one is
renamed to `gsc.exe.old` and deleted on a later run). `--yes` skips the question; `--no-input` requires
`--yes`. If the install directory is not writable it stops and leaves the old binary in place. A `gsc` in a
Go bin directory (`$GOBIN`, `$GOPATH/bin`, `~/go/bin`) came from a source build, so `gsc update` tells you to
run `git pull && make install` in your checkout instead of replacing it. `--read-only` blocks installing but
allows `--check`.

In an interactive terminal, `gsc` checks the github.com release page for a new release at most once a day
(not the GitHub API, so its per-IP rate limit never breaks the check on shared networks) and, after a
command's output, prints a notice on stderr:

```text
A new version of gsc is available: v0.1.4 -> v0.1.5
Update with: gsc update
Release notes: https://github.com/piyush-gambhir/gsc-cli/releases/tag/v0.1.5
```

It never checks (no network, no output) when stderr is not a terminal, when `CI` is set, with `--quiet` or
`GSC_QUIET`, or when `GSC_NO_UPDATE_NOTIFIER=1` or `NO_UPDATE_NOTIFIER=1` is set, so scripts, CI, and agents
are not affected. Only the one command a day that runs the check waits for it, at most 1 second after its
output. `gsc version` shows the last known latest version from that check without a network call.

### Verify a download

Releases are immutable once published and ship SBOMs plus signed build-provenance attestations. To confirm an
archive was built by this repository's release workflow:

```bash
gh attestation verify gsc-cli_darwin_arm64.tar.gz --repo piyush-gambhir/gsc-cli
```

## Quick start

```bash
gsc auth login                          # opens your browser once; tokens refresh automatically
gsc sites list                          # properties and your permission level
gsc performance --last 28d --compare previous
gsc top queries --last 3m --limit 50
gsc query -d query -d page --filter 'page =~ ^https://example.com/blog/' --limit 5000 -o csv > blog.csv
gsc trend --last 16m --by month
gsc inspect https://www.example.com/pricing
gsc insights striking-distance --last 28d
gsc export --last 16m -d query -d page --out ./gsc-export
```

The default site comes from the profile (set at login or with `gsc sites use`). Override with `-s`:
`sc-domain:example.com`, `https://www.example.com/`, or a bare host such as `example.com`, which is matched
against your properties.

## Commands

| Command | Purpose |
| --- | --- |
| `auth login`, `login` | Sign in (browser), or save a service account, ADC, or your own OAuth client |
| `auth status`, `status` | Show the credential in use without revealing it (`--verify` makes one call) |
| `auth list`, `auth use NAME`, `auth logout [--revoke]`, `auth token` | Manage profiles |
| `config show`, `config list-profiles`, `config use-profile NAME` | Configuration aliases consistent with the other CLIs |
| `sites list`, `sites get`, `sites use` | Properties and the default site |
| `sites add`, `sites remove` | Add or remove a property from your list (writes) |
| `sitemaps list`, `sitemaps get`, `sitemaps submit`, `sitemaps delete` | Sitemaps (submit and delete are writes) |
| `query` | Search Analytics with every API option |
| `performance` | Totals for a period, with `--compare previous` or `yoy` |
| `top queries\|pages\|countries\|devices\|appearance` | Top rows for one dimension |
| `trend --by date\|hour\|week\|month` | Time series; week and month are rolled up locally |
| `freshness` | Latest dates with final and with preliminary data |
| `inspect URL...` | URL Inspection (Google's indexed view, not a live test) |
| `export` | Day-by-day CSV or NDJSON files, resumable |
| `insights striking-distance\|low-ctr\|cannibalization\|decliners\|new-queries\|lost-queries` | Opportunities computed locally, thresholds printed |
| `api METHOD PATH` | Raw authenticated request (escape hatch for new API methods) |
| `doctor` | Check config, credentials, keychain, scopes, and `PATH` (`--online` adds one API call) |
| `completion`, `version`, `update [--check]` | Shell completion, build info, self-update |

The complete flag reference is [docs/commands.md](docs/commands.md), generated from the command tree.

## Authentication

`gsc auth login` is the easy path: it opens Google's consent page, stores the refresh token in the OS keychain,
and picks your default site. Other methods, all described in [docs/auth.md](docs/auth.md):

- `--no-browser` for SSH sessions (approve on any device, paste the redirected URL back).
- `--client-secret-file` or `GSC_CLIENT_ID`/`GSC_CLIENT_SECRET` for your own OAuth client.
- `--service-account key.json` (with optional `--subject` for domain-wide delegation) for CI.
- `--adc` and `--adc --impersonate SA_EMAIL` for gcloud, workload identity federation, and keyless CI.
- `GSC_CREDENTIALS` (any Google credential JSON) or `GSC_ACCESS_TOKEN` per command.

Tokens are never printed except by `gsc auth token`, never logged, and never sent through HTTP redirects.
Without a keychain service, login stops and offers `--insecure-storage` (a 0600 file) rather than writing
plaintext silently.

## Search Analytics notes

- Dates are Search Console's timezone, Pacific Time, inclusive. `--last 28d` ends at the latest day with
  data (one small extra request finds it; pass `--end` to skip). Month windows ending on a month's last day
  cover whole months.
- `--limit` defaults to 1,000 rows (25 for `top`); pages of up to 25,000 are fetched with `startRow`.
  `--all` fetches every row Google exposes. `complete: true` in JSON means pagination reached the end; it
  never means every query, because Google withholds anonymized queries and exposes at most 50,000 rows per
  day per search type.
- `--data-state all` includes preliminary data; JSON reports `first_incomplete_date` (or `_hour`) when
  Google provides it. `hour` requires `--data-state hourly_all` and covers about 10 days.
- `--compare` runs a second query for the previous period or the same dates a year earlier. Rows missing
  from one period are `null`, not zero; CTR changes are in percentage points; percent change is `null` when
  the base is zero.
- Filters: `--filter 'DIM OP VALUE'` with `=`, `!=`, `~` (contains), `!~`, `=~` (RE2 regex), `!=~`. Repeated
  filters are ANDed; the API has no OR. `page` and `query` equality is case-sensitive.
- Never sum query rows to get site totals; use `gsc performance`.

## Output and automation

`-o table` (default), `-o json`, `-o yaml`, or `-o csv` (row-shaped results; text starting with `=`, `+`,
`-`, `@` gets a leading apostrophe so spreadsheets do not run it as a formula). JSON for analytics commands
is an envelope with the site, dates, type, data state, completeness, and rows. Errors in JSON/YAML mode are
structured on stderr with a `kind` (`auth`, `permission`, `not_found`, `rate_limit`, `usage`, ...); the exit
status is 0 on success and 1 on failure. `gsc commands -o json` describes every command, flag, and effect
offline, and `gsc api methods -o json` lists the API methods and the commands that call them.

`--read-only` (or `GSC_READ_ONLY=1`) blocks every remote write, local credential change, and self-update.
`--dry-run` prints a write request without sending it (`--read-only` refuses it even then). Destructive commands and `update` confirm, or need
`--yes` (`-y`) with `--no-input`. No command retries automatically; `export --retry N` opts in.

| Environment variable | Meaning |
| --- | --- |
| `GSC_PROFILE` | Profile to use |
| `GSC_SITE` | Default property for this shell |
| `GSC_ACCESS_TOKEN` | Raw access token (highest precedence) |
| `GSC_CREDENTIALS` | Google credential JSON file |
| `GSC_CLIENT_ID`, `GSC_CLIENT_SECRET` | Override the built-in OAuth client |
| `GSC_CONFIG`, `XDG_CONFIG_HOME` | Config location (default `~/.config/gsc-cli/config.yaml`) |
| `GSC_NO_INPUT`, `GSC_QUIET`, `GSC_VERBOSE`, `GSC_READ_ONLY` | Same as the flags (`1` or `true`) |
| `GSC_NO_UPDATE_NOTIFIER`, `NO_UPDATE_NOTIFIER` | Turn off the daily release check and update notice (any value) |

## API coverage and limits

All 10 active Search Console API methods are implemented; the retired Mobile-Friendly Test and the separate
Indexing API are skipped with reasons. [docs/api-coverage.md](docs/api-coverage.md) maps every method to its
commands, and [docs/compatibility.md](docs/compatibility.md) records the pinned API snapshot (discovery
revision 20261005, captured 2026-10-07) and how to refresh it.

Quotas that matter: Search Analytics 1,200 queries per minute per site plus unpublished load quotas (page plus
query grouping over long ranges is expensive), and URL Inspection 2,000 per day and 600 per minute per site.
`inspect` paces itself and stops cleanly at the quota.

## Development

```bash
make build
make test       # race-enabled; fake transports, no Google account needed
make vet
make docs       # regenerate docs/commands.md
```

The docs site lives in `web/` (Next.js and Fumadocs, exported as static files). Its command reference and API
coverage pages are generated from `docs/` at build time. `cd web && pnpm install && pnpm dev` runs it locally;
`scripts/deploy-docs.sh` deploys it.

Layout follows the CLI suite: Go in `cli-go/`, docs in `docs/`, agent skill in `gsc/` (install with
`npx skills add piyush-gambhir/gsc-cli@gsc`), docs site in `web/`,
workflows in `.github/`. See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md),
[CLAUDE.md](CLAUDE.md), [PLAN.md](PLAN.md), and [RESEARCH.md](RESEARCH.md). MIT licensed.
