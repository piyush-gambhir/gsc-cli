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
`--read-only`, as are local credential changes and self-update. Release checksums detect corrupted or
mismatched downloads; they are not a separate publisher signature.

Google controls the upstream APIs. Report Google service vulnerabilities through Google's programs.
