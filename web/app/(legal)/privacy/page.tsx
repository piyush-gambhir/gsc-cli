import type { Metadata } from 'next';
import Link from 'next/link';
import { LegalPage } from '@/components/legal-page';
import { createPageMetadata } from '@/lib/metadata';

export const metadata: Metadata = createPageMetadata({
  title: 'Privacy Policy',
  description:
    'Privacy policy for gsc-cli, an independent, unofficial open-source CLI for Google Search Console.',
  path: '/privacy',
});

const userDataPolicy = 'https://developers.google.com/terms/api-services-user-data-policy';
const limitedUse = `${userDataPolicy}#additional_requirements_for_specific_api_scopes`;

export default function PrivacyPage() {
  return (
    <LegalPage title="Privacy Policy" effective="October 5, 2026">
      <p className="legal-page__lede">
        gsc-cli (the <code>gsc</code> command-line tool) is an open-source
        program that runs entirely on your own computer. It does <strong>not</strong>{' '}
        collect, transmit, or store your personal data or your Google data on any
        server operated by its developer, Piyush Gambhir (&quot;the developer&quot;).
      </p>

      <h2>1. No data collection</h2>
      <p>
        The CLI runs locally on your machine. The developer operates{' '}
        <strong>no backend servers</strong> for <code>gsc</code> and receives{' '}
        <strong>no data</strong> from your use of the tool. There is no analytics, no
        telemetry, no tracking, and no advertising.
      </p>

      <h2>2. Google data gsc accesses</h2>
      <p>
        When you sign in with <code>gsc auth login</code>, <code>gsc</code> asks Google for
        these permissions:
      </p>
      <ul>
        <li>
          <code>https://www.googleapis.com/auth/webmasters</code>: view and manage Search
          Console data for your sites. If you log in with <code>--scope readonly</code>,{' '}
          <code>gsc</code> requests <code>https://www.googleapis.com/auth/webmasters.readonly</code>{' '}
          (view only) instead.
        </li>
        <li>
          <code>openid</code> and <code>email</code>: your Google account&apos;s email address
          and account identifier, so <code>gsc</code> can show which account you are signed
          in as.
        </li>
      </ul>
      <p>
        With these permissions <code>gsc</code> can read the Search Console properties you
        can access and your permission level for each, search performance data (queries,
        pages, countries, devices, search appearance, dates, clicks, impressions,
        click-through rate, and average position), URL Inspection results, and sitemaps.
        With the full scope it can also add and remove properties and submit and delete
        sitemaps.
      </p>

      <h2>3. How gsc uses that data</h2>
      <p>
        <code>gsc</code> uses Google data only to perform the commands you run, on your own
        computer:
      </p>
      <ul>
        <li>
          Search Console data is requested when you run a command and is shown in your
          terminal, or written to files you choose (for example with{' '}
          <code>-o csv &gt; file.csv</code> or <code>gsc export --out DIR</code>).
        </li>
        <li>
          Your email address and account identifier are saved in your local configuration to
          label the profile, and shown by commands such as <code>gsc auth status</code>.
        </li>
        <li>
          Changes (adding or removing a property, submitting or deleting a sitemap) happen
          only when you run the command that makes them. <code>--read-only</code> blocks them.
        </li>
      </ul>
      <p>
        <code>gsc</code> does not use Google data for advertising, does not sell it, and does
        not use it to develop, improve, or train AI or machine learning models. The developer
        cannot see your data, and no one reads it on the developer&apos;s behalf.
      </p>

      <h2>4. Google API Services User Data Policy</h2>
      <p>
        <code>gsc</code>&apos;s use and transfer of information received from Google APIs will
        adhere to the{' '}
        <a href={userDataPolicy} target="_blank" rel="noreferrer">
          Google API Services User Data Policy
        </a>
        , including the{' '}
        <a href={limitedUse} target="_blank" rel="noreferrer">
          Limited Use requirements
        </a>
        .
      </p>

      <h2>5. Storage &amp; security</h2>
      <p>Everything <code>gsc</code> keeps stays on your computer:</p>
      <ul>
        <li>
          <strong>Sign-in tokens</strong> are stored in your operating system&apos;s keychain
          (macOS Keychain, Windows Credential Manager, or the Secret Service on Linux), along
          with the client secret if you log in with your own OAuth client. They go to a
          plaintext local file instead (<code>secrets.yaml</code>, with owner-only{' '}
          <code>0600</code> permissions) only if you choose it, either with{' '}
          <code>--insecure-storage</code> or by confirming <code>gsc</code>&apos;s offer when the
          keychain cannot be used during an interactive login; otherwise a missing keychain
          stops the login.
        </li>
        <li>
          <strong>Configuration</strong> lives in <code>~/.config/gsc-cli/config.yaml</code>{' '}
          (or under <code>GSC_CONFIG</code> or <code>XDG_CONFIG_HOME</code>), readable only by
          you. It holds profile names, the signed-in email address and account identifier,
          the granted scopes, the OAuth client ID used, and your default property. For a
          service account it holds the key file&apos;s path, never the key.
        </li>
        <li>
          <strong>Search Console data</strong> is not kept, except in files you ask{' '}
          <code>gsc</code> to write. <code>gsc export</code>, for example, writes data files
          and a <code>manifest.json</code> to the directory you name.
        </li>
      </ul>
      <p>
        Tokens travel only to Google, over HTTPS. <code>gsc</code> never writes them to logs
        or error messages, never sends them through HTTP redirects, and prints one only when
        you run <code>gsc auth token</code>.
      </p>

      <h2>6. Network connections</h2>
      <p>The CLI makes outbound network requests only while you run a command, and only to:</p>
      <ul>
        <li>
          <strong>Google</strong>, to sign you in and perform the actions you request:{' '}
          <code>accounts.google.com</code> (the consent page, opened in your browser),{' '}
          <code>oauth2.googleapis.com</code> (tokens and revocation),{' '}
          <code>searchconsole.googleapis.com</code> (the Search Console API), and{' '}
          <code>iamcredentials.googleapis.com</code> when you use <code>--impersonate</code>.
          With Application Default Credentials or a credentials file, Google&apos;s
          authentication library also contacts the token sources that credential names, such
          as the metadata server on Google Cloud. During browser login, <code>gsc</code>{' '}
          receives Google&apos;s reply on <code>127.0.0.1</code>, on your own computer.
        </li>
        <li>
          <strong>GitHub</strong> (github.com release pages, not the GitHub API), when you
          run <code>gsc update</code>, to find and download the latest release, and for the
          update notice described below. These requests contain no personal data.
        </li>
      </ul>
      <p>
        The only background request is the update check: at most once a day, and only
        when the CLI runs in an interactive terminal, it reads the latest release number
        from the github.com release page so it can tell you a new version is out. It never
        runs when the CLI&apos;s error output is not a terminal (as for most scripts and
        coding agents), in CI (when <code>CI</code> is set), or with <code>--quiet</code>, and
        you can turn it off by setting <code>GSC_NO_UPDATE_NOTIFIER=1</code> or{' '}
        <code>NO_UPDATE_NOTIFIER=1</code>. The developer is not a party to, and cannot
        observe, any of these connections.
      </p>

      <h2>7. Sharing &amp; third parties</h2>
      <p>
        Your Search Console data goes only between Google and your computer. It is never
        sent to the developer or to any third party. The developer does not sell, rent, or
        share any data, and the tool integrates no third-party analytics, advertising, or
        tracking SDKs. Your use of Google Search Console is governed by Google&apos;s own
        terms and privacy policy. If you pass <code>gsc</code>&apos;s output to another
        program, such as a spreadsheet or a coding agent, that program&apos;s terms and
        privacy policy apply to what you give it.
      </p>

      <h2>8. Revoking access &amp; deleting data</h2>
      <ul>
        <li>
          <code>gsc auth logout</code> deletes a profile and its saved tokens from your
          computer.
        </li>
        <li>
          <code>gsc auth logout --revoke</code> also revokes <code>gsc</code>&apos;s access at
          Google. Google ends that Google account&apos;s grant for every client in the OAuth
          client&apos;s project, which signs the account out on every machine and profile using
          it, so <code>gsc</code> asks before doing it.
        </li>
        <li>
          You can remove <code>gsc</code>&apos;s access at any time from your Google Account
          at{' '}
          <a href="https://myaccount.google.com/permissions" target="_blank" rel="noreferrer">
            myaccount.google.com/permissions
          </a>
          .
        </li>
        <li>
          To remove everything, run <code>gsc auth logout</code> for each profile
          (<code>gsc auth list</code> shows them) so their tokens leave the keychain, then
          delete the configuration directory (<code>~/.config/gsc-cli/</code> by default) and
          any files you exported.
        </li>
      </ul>
      <p>
        Because the developer never receives your data, there is nothing for the developer to
        retain or delete: you control all of it.
      </p>

      <h2>9. Children</h2>
      <p>This tool is a developer utility and is not directed at children under 13.</p>

      <h2>10. Changes to this policy</h2>
      <p>Any changes will be posted on this page with an updated effective date.</p>

      <h2>11. Contact</h2>
      <p>
        Questions about this policy? Contact <strong>Piyush Gambhir</strong> at{' '}
        <a href="mailto:developer.piyushgambhir@gmail.com">
          developer.piyushgambhir@gmail.com
        </a>
        , or see the <Link href="/contact">contact page</Link>.
      </p>
    </LegalPage>
  );
}
