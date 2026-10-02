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

Run `goreleaser check --config cli-go/.goreleaser.yaml`, run CI, set `cli-go/VERSION`, and push a signed
version tag. The release workflow reads the built-in OAuth client from the `GSC_OAUTH_CLIENT_ID` and
`GSC_OAUTH_CLIENT_SECRET` repository secrets (see `docs/auth.md`). A local packaging check is
`goreleaser release --snapshot --clean --skip=publish --config cli-go/.goreleaser.yaml`.
