import type { Metadata } from 'next';
import Link from 'next/link';
import { LegalPage } from '@/components/legal-page';
import { createPageMetadata } from '@/lib/metadata';

export const metadata: Metadata = createPageMetadata({
  title: 'Contact',
  description:
    'Contact information for Search Console CLI (gsc), an independent, unofficial open-source CLI for Google Search Console.',
  path: '/contact',
});

export default function ContactPage() {
  return (
    <LegalPage title="Contact">
      <p className="legal-page__lede">
        Search Console CLI is a free, open-source project maintained by{' '}
        <strong>Piyush Gambhir</strong>. Support is best-effort; here are the best
        ways to get in touch.
      </p>

      <div className="legal-contact-grid">
        <section>
          <p className="legal-contact-grid__label">Email</p>
          <p className="legal-contact-grid__value">
            <a href="mailto:developer.piyushgambhir@gmail.com">
              developer.piyushgambhir@gmail.com
            </a>
          </p>
          <p>General questions, privacy, and security reports.</p>
        </section>
        <section>
          <p className="legal-contact-grid__label">Bugs &amp; features</p>
          <p className="legal-contact-grid__value">
            <a
              href="https://github.com/piyush-gambhir/gsc-cli/issues"
              target="_blank"
              rel="noreferrer"
            >
              GitHub Issues ↗
            </a>
          </p>
          <p>The fastest way to report a bug or request a feature.</p>
        </section>
        <section>
          <p className="legal-contact-grid__label">Source</p>
          <p className="legal-contact-grid__value">
            <a
              href="https://github.com/piyush-gambhir/gsc-cli"
              target="_blank"
              rel="noreferrer"
            >
              piyush-gambhir/gsc-cli ↗
            </a>
          </p>
          <p>Read the code, open a pull request, or fork it.</p>
        </section>
      </div>

      <h2>Security issues</h2>
      <p>
        If you believe you&apos;ve found a security vulnerability, please email{' '}
        <a href="mailto:developer.piyushgambhir@gmail.com">
          developer.piyushgambhir@gmail.com
        </a>{' '}
        with the details rather than opening a public issue, or use GitHub&apos;s private vulnerability
        reporting on the repository. Search Console CLI keeps your Google sign-in only on your own device
        and operates no servers, but responsible disclosure is always appreciated.
      </p>

      <h2>Response time</h2>
      <p>
        This is an independent side project, not a commercial product. The maintainer
        aims to respond when possible, but no response time or level of support is
        guaranteed. See the <Link href="/terms">Terms of Service</Link> for the full
        no-warranty terms.
      </p>

      <h2>Not affiliated with Google</h2>
      <p>
        Search Console CLI is an independent, unofficial tool and is not affiliated
        with, endorsed by, or sponsored by Google. For issues with Google Search
        Console itself, use Google&apos;s own support channels.
      </p>
    </LegalPage>
  );
}
