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
- Document user-visible changes (README, docs, skill) and regenerate `docs/commands.md` when flags change.
