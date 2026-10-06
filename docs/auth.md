# Authentication

Search Console data belongs to a Google account, so every request needs an OAuth access token. API keys do
not work for this API, and Google's device-code flow does not allow Search Console scopes. `gsc` supports
every other way to get a token.

| Method | Command | Best for | What is stored |
| --- | --- | --- | --- |
| Browser login (default) | `gsc auth login` | People on a machine with a browser | Refresh token in the OS keychain |
| Headless browser login | `gsc auth login --no-browser` | SSH sessions, containers | Same |
| Your own OAuth client | `gsc auth login --client-secret-file client_secret.json` | Teams wanting their own quota or branding | Refresh token and client secret in the keychain |
| Service account | `gsc auth login --service-account key.json` | CI, servers | Path to the key file (no secret copied) |
| Application Default Credentials | `gsc auth login --adc` | gcloud users, GitHub Actions federation, GCE/Cloud Run | Nothing |
| Impersonation | `gsc auth login --adc --impersonate SA_EMAIL` | Keyless CI, least privilege | Nothing |
| Credentials file per command | `--credentials FILE` or `GSC_CREDENTIALS` | One-off automation | Nothing |
| Raw access token | `--access-token` or `GSC_ACCESS_TOKEN` | Scripts, debugging | Nothing |

Credential precedence for every command: `--access-token` / `GSC_ACCESS_TOKEN`, then `--credentials` /
`GSC_CREDENTIALS`, then the selected profile. Profile selection: `--profile`, then `GSC_PROFILE`, then the
saved current profile. `gsc` never falls back to ambient Application Default Credentials on its own, so an
agent never runs as an identity nobody chose.

## Browser login

```console
$ gsc auth login
Opening your browser to sign in. If it does not open, visit:
  https://accounts.google.com/o/oauth2/v2/auth?...
Logged in as me@example.com (profile "default").
Found 3 properties. Default site set to sc-domain:example.com (change with: gsc sites use SITE).
```

1. `gsc` listens on `127.0.0.1` on a free port and opens Google's consent page (PKCE S256, random `state`,
   offline access). The URL is printed to stderr in case the browser does not open.
2. Approve access on Google's consent screen. The tab then confirms you are signed in; close it and return to
   the terminal.
3. The refresh token is saved in the OS keychain (macOS Keychain, Windows Credential Manager, or Linux Secret
   Service). Access tokens refresh automatically from then on. Keychain entries are namespaced by config
   file: the default config uses the service `gsc-cli`, and any other path (`GSC_CONFIG`, a custom
   `XDG_CONFIG_HOME`, or one config per environment) gets `gsc-cli (<hash>)`, so same-named profiles in
   different configs never overwrite each other.
4. `gsc` lists your properties once and picks a default site: the only property, your choice from a list
   (interactive), or the first domain property (`--no-input`). `--no-verify` skips this call.

Options:

- `--profile NAME` saves under a named profile (default: the current profile, or `default`).
- `--scope readonly` requests `webmasters.readonly` only. Write commands then stop early with a message.
  The default `full` scope covers every command; `--read-only` still blocks writes for agents.
- `--insecure-storage` keeps the token in `secrets.yaml` (mode 0600) next to the config instead of the
  keychain. Google's OAuth policy asks for encrypted storage, so use this only where no keychain exists.
  Without the flag, a missing keychain stops the login (and asks first when you are at a terminal).

Re-run `gsc auth login` any time Google revokes access; errors say exactly which command to run.

## Headless login (`--no-browser`)

For a machine without a browser:

1. Run `gsc auth login --no-browser` and open the printed URL on any device.
2. Approve access. The browser then tries to load `http://127.0.0.1:8085/?code=...` and fails. That is
   expected.
3. Copy the full address from the browser and paste it into the terminal.

This uses the same loopback redirect and PKCE as the normal flow. It is not Google's retired out-of-band
(copy-a-code) flow.

## Your own OAuth client (`--client-secret-file`)

1. In Google Cloud Console, create or pick a project and enable the **Google Search Console API**.
2. Configure the OAuth consent screen (External or Internal), then create an OAuth client of type
   **Desktop app** and download its JSON.
3. Run `gsc auth login --client-secret-file client_secret_XXXX.json`.

Alternatively set `GSC_CLIENT_ID` and `GSC_CLIENT_SECRET`; that overrides the built-in client for every login.
Each profile records which client it used, so refreshes always use the same one. Keep the consent screen
**In production**: Google expires refresh tokens after 7 days for apps left in Testing.

## Service accounts

1. In Google Cloud Console, enable the Search Console API, create a service account, and create a JSON key.
2. In Search Console, open the property, then **Settings > Users and permissions > Add user**, and add the
   service account email (`...@...iam.gserviceaccount.com`) with **Full** permission (Owner only if you need
   owner-only actions).
3. Run `gsc auth login --profile ci --service-account /path/key.json`.

`gsc` stores the key file path, not the key. Google Workspace admins can use domain-wide delegation with
`--subject user@yourdomain.com`; the impersonated user must have access to the property.

For a single command without a profile: `GSC_CREDENTIALS=/path/key.json gsc performance -s ...`.

## Application Default Credentials and impersonation

`gsc auth login --adc` saves a profile that resolves Google's Application Default Credentials at run time:
`GOOGLE_APPLICATION_CREDENTIALS`, the gcloud ADC file, workload identity federation, or the GCE/Cloud Run
metadata server.

gcloud's own client is meant for Google Cloud scopes, so log in with your Desktop client and the Search
Console scope:

```bash
gcloud auth application-default login \
  --client-id-file=client_secret.json \
  --scopes=https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/webmasters.readonly
gsc auth login --profile gcloud --adc
```

Keyless CI (for example GitHub Actions with `google-github-actions/auth`): give the federated identity
`roles/iam.serviceAccountTokenCreator` on a service account, add that service account to the Search Console
property, then:

```bash
gsc auth login --profile ci --adc --impersonate reporting@my-project.iam.gserviceaccount.com --no-verify
```

## Raw access tokens

`GSC_ACCESS_TOKEN=$(gcloud auth print-access-token) gsc sites list` works with any token that carries a
Search Console scope. Nothing is stored and nothing refreshes. `gsc auth token` prints the current profile's
token for debugging; treat it as a secret.

## Status, profiles, logout

```bash
gsc auth status            # which credential is in use, from local config only
gsc auth status --verify   # also lists properties once
gsc auth list              # profiles, without secrets
gsc auth use work          # switch the default profile
gsc auth logout            # delete this profile and its token locally
gsc auth logout --revoke   # also revoke at Google (asks; --yes to skip)
```

`--revoke` is opt-in because Google revokes the account's tokens for every client in the OAuth project,
which signs out every other machine and profile using the same client.

`--read-only` blocks every command that changes local credentials or configuration, as well as remote
writes; tokens refreshed in read-only mode stay in memory.

## Owner setup: the built-in OAuth client

The one-command login needs an OAuth client compiled into release binaries. The maintainer creates it once:

1. Create a Google Cloud project (for example `gsc-cli`) and enable the **Google Search Console API**.
2. Configure the OAuth consent screen: **External**, app name, support email, and developer contact. Under
   **Branding**, set the home page and privacy policy links and add their domain as an authorized domain;
   **Publish app** stays disabled until they are set. Under **Data Access**, add `openid`, `.../auth/userinfo.email`,
   `.../auth/webmasters`, and `.../auth/webmasters.readonly`. Then set the publishing status to **In production**.
3. Create an OAuth client of type **Desktop app**.
4. Add the client ID and secret as repository secrets `GSC_OAUTH_CLIENT_ID` and `GSC_OAUTH_CLIENT_SECRET`. The
   release workflow passes them to GoReleaser, which injects them with `-ldflags -X`.
5. For local builds, put them in the untracked `cli-go/.env.local`:
   ```make
   GSC_OAUTH_CLIENT_ID=1234-abc.apps.googleusercontent.com
   GSC_OAUTH_CLIENT_SECRET=GOCSPX-...
   ```
   `make build` and `make install` read that file. `gsc version -o json` reports `builtin_oauth_client: true`.

Never commit these values: Google's policy forbids client credentials in public repositories. Desktop-app
secrets are not confidential in Google's model (PKCE protects the flow), which is why shipping them inside the
binary is acceptable.

Google classifies both Search Console scopes as non-sensitive, so the published app needs no Google
verification, shows no unverified-app notice, and has no user cap. That holds while the consent screen has no
logo, lists at most 10 authorized domains, and requests no sensitive or restricted scope; changing any of those
requires verification. Project quotas are shared by everyone using the built-in client; heavy users should bring
their own client or a service account.

## Troubleshooting

| Message | Fix |
| --- | --- |
| `this build has no built-in OAuth client` | Use a release build, set `GSC_CLIENT_ID`/`GSC_CLIENT_SECRET`, or pass `--client-secret-file`. |
| `the OS keychain is unavailable` | Start a Secret Service (for example gnome-keyring), or re-run login with `--insecure-storage`, or use `GSC_ACCESS_TOKEN` / `GSC_CREDENTIALS`. |
| `OS keychain did not respond within 10s` | Unlock the keychain and retry. |
| `Google rejected the saved credentials (invalid_grant ...)` | Run `gsc auth login --profile NAME`. Causes: revoked access, password change, or a Testing-mode client. |
| `The signed-in identity lacks access to this property` | Check `gsc sites list`, or add the user or service account in Search Console. |
| `Enable the Google Search Console API ...` | Enable the API in the OAuth client's Google Cloud project. |
| `another gsc is on PATH` (doctor) | Ghostscript and Gambit Scheme also install `gsc`. Put this binary's directory first on `PATH`, or alias it. |
