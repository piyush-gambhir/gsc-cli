# gsc CLI agent guide

Use `gsc --help` and `docs/commands.md` for current flags. See `gsc/SKILL.md` for operating guidance,
`docs/auth.md` for every login method, and `docs/api-coverage.md` for the API-to-command map.

- Implementation: Go/Cobra in `cli-go/`; module `github.com/piyush-gambhir/gsc-cli/cli-go`.
- `make test`, `make vet`, `make build`, and `make docs` run from the repo root.
- Auth: browser OAuth (built-in client injected at build time via `-ldflags`, never committed), headless
  paste flow, BYO client, service accounts, ADC, impersonation, credentials files, raw tokens. Refresh tokens
  live in the OS keychain; plaintext only with `--insecure-storage`.
- Tests use fake transports, `keyring.MockInit()`, and temporary configs. Never contact Google from tests.
- Classify commands by effect, not HTTP method: `query` and `inspect` POST but only read. New commands need
  `mutates`, `writes-local`, or `interactive` annotations; `TestAgentSafetyCommandManifest` pins the set.
- `TestAPICoverage` ties the vendored discovery snapshot, `internal/coverage`, the command tree, and
  `docs/api-coverage.md` together. Update all of them when the API or commands change.
- Preserve stdout as data and stderr as diagnostics. No automatic retries outside `export --retry`. Never
  present `all` data as final, never sum query rows as totals, never call URL Inspection a live test.
- No background calls except one: the update notice's release check (`cmd/notify.go`, `internal/update`). It
  runs at most once a day, only when stderr is a terminal, never for `update`, `version`, `completion`, `help`,
  or dev builds, and is off under `CI`, `--quiet`, `GSC_NO_UPDATE_NOTIFIER`, or `NO_UPDATE_NOTIFIER`. Only the
  one run a day that sends the check waits for it, at most `update.NoticeWait` (1s) after the output; a cached
  answer never waits. Do not add others.
- Document user-visible changes (README, docs, skill) and regenerate `docs/commands.md` when flags change.
- Releases: bump `cli-go/VERSION` in a pull request; merging to `main` tags and publishes it (see
  CONTRIBUTING.md). Never push release tags by hand. `main` requires pull requests and passing checks.
