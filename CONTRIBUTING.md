# Contributing

Use Go 1.26+ (toolchain Go 1.27.1). From the repository root:

```bash
make test
make vet
make build
make docs
```

Run `gofmt -w .` within `cli-go/` after Go changes. HTTP tests must use fake transports or local test servers
and must not contact Google. Keychain tests use `keyring.MockInit()`. Configuration tests use temporary
directories. Never commit OAuth client secrets, refresh tokens, service account keys, or exported customer data.

Endpoint contracts live in `internal/client`, credentials in `internal/auth` and `internal/secrets`, query
building and analysis in `internal/analytics`, and command composition in `cmd`. Keep local validation ahead
of credential resolution and network requests.

When adding or reclassifying a command, update its annotations and the digest in
`cmd/agent_safety_manifest_test.go`. When Google changes the API, follow the refresh procedure in
`docs/compatibility.md`; `TestAPICoverage` lists what needs mapping.

Submit changes through a pull request. Commits must be signed. The default branch requires linear history,
resolved review threads, and passing Go CI and CodeQL checks.

## Dependency maintenance

Prefer current stable releases, pinned to exact module versions and immutable GitHub Action commit SHAs.
Dependabot checks daily and groups minor and patch updates per ecosystem. Run `go get -u -t ./...` and
`go mod tidy` inside `cli-go`, then run platform CI. Check the GoReleaser version in both CI and release
workflows.

## Releases

To release, change `cli-go/VERSION` in a pull request. When it merges into `main`, the release workflow tags
`vX.Y.Z` on the merge commit, runs the tests and `govulncheck`, builds with GoReleaser (archives, checksums,
SBOMs), attests build provenance, and only then publishes the release. Published releases are immutable, so
a mistake needs a new version. Rerun a failed release from the Actions tab (Release, Run workflow); a
version that is already published is skipped. Do not push tags by hand. Release builds read the built-in
OAuth client from the `GSC_OAUTH_CLIENT_ID` and `GSC_OAUTH_CLIENT_SECRET` repository secrets (see
`docs/auth.md`). After changing `.goreleaser.yaml`, run `goreleaser check --config cli-go/.goreleaser.yaml`;
a local packaging check is `goreleaser release --snapshot --clean --skip=publish --config cli-go/.goreleaser.yaml`
(SBOMs need Syft installed).

`main` is protected: changes land through pull requests (squash or rebase) with signed commits, linear
history, and passing CI (tests on Linux, macOS, and Windows, staticcheck, govulncheck, the release check) and
CodeQL. Dependabot minor and patch updates merge automatically once those checks pass; major updates wait for
review. Release tags cannot be moved or deleted.
