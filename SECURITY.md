# Security policy

Security fixes target the latest release. Update before reporting a potential issue.

Please report vulnerabilities privately using [GitHub private vulnerability reporting](https://github.com/piyush-gambhir/gsc-cli/security/advisories/new).
Do not include real tokens, service account keys, or Search Console data in a public issue.

How credentials are handled:

- Refresh tokens are stored in the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret
  Service). A plaintext file (mode 0600) is used only when a login explicitly passes `--insecure-storage`.
- Service account keys are never copied; profiles store the key file path.
- Tokens, authorization codes, and client secrets are redacted from errors and never written to verbose logs.
  `gsc auth token` prints a token only because it is asked to.
- Browser login uses a loopback listener on 127.0.0.1, PKCE (S256), and a random `state`.
- HTTP redirects are never followed with credentials attached.
- The built-in OAuth client is a Google "Desktop app" client whose secret Google does not treat as
  confidential; it is injected at release time and never committed.

Remote writes (`sites add/remove`, `sitemaps submit/delete`, and non-read `gsc api` calls) are blocked by
`--read-only`, as are local credential changes and self-update. `gsc update` replaces the local executable
after SHA-256 checksum verification, extracting only the `gsc` (or `gsc.exe`) regular file from the archive; a
failed update leaves the old binary in place.

In an interactive terminal, `gsc` reads the latest release from the github.com release page at most once a
day (an anonymous request with no account or usage data; not the GitHub API) to print an update notice.
`gsc update` uses the same page and downloads from github.com release downloads. It never runs when stderr is
not a terminal or `CI` is set, and `GSC_NO_UPDATE_NOTIFIER=1`, `NO_UPDATE_NOTIFIER=1`, or `--quiet` turns it
off. The result is cached in `update-check.json` in the config directory.

Releases are immutable once published and include SBOMs and signed build-provenance attestations; verify an
archive with `gh attestation verify <archive> --repo piyush-gambhir/gsc-cli`. `gsc update` and `install.sh`
also check SHA-256 checksums.

Google controls the upstream APIs. Report Google service vulnerabilities through Google's programs.
