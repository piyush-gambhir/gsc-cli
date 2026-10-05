import { ArrowRight } from 'lucide-react';
import Link from 'next/link';
import { HomeHero } from '@/components/home-hero';
import { InstallCommand } from '@/components/install-command';
import { Reveal } from '@/components/reveal';
import { SiteFooter } from '@/components/site-footer';
import { OsmoButton } from '@/components/ui/osmo-button';
import { site } from '@/lib/site';
import { siteUrl } from '@/lib/shared';
import { getOtherSuiteProjects } from '@/lib/suite';

const revealDelays = ['0s', '0.075s', '0.15s'] as const;

// What gsc does with Google user data. Google's OAuth verification asks the
// homepage to state this; keep it in step with the privacy policy.
const dataUse = [
  {
    title: 'What it asks for',
    body: 'Search Console access (the webmasters scope, or webmasters.readonly with --scope readonly) and your email address, so gsc can show which account is signed in.',
  },
  {
    title: 'What it does with it',
    body: 'Lists your properties, reads performance and URL Inspection results, and adds or removes sitemaps and properties, each only when you run that command.',
  },
  {
    title: 'Where it goes',
    body: 'Your data travels only between Google and your computer. It is never sent to the developer or anyone else, and your login stays in your OS keychain.',
  },
] as const;

export default function HomePage() {
  const relatedLink = getOtherSuiteProjects(site.repo).map(({ href }) => href);
  const structuredData = {
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'SoftwareApplication',
        name: site.name,
        applicationCategory: 'DeveloperApplication',
        operatingSystem: 'macOS, Linux, Windows',
        description: site.description,
        url: siteUrl,
        downloadUrl: `https://github.com/${site.repo}/releases`,
        license: `https://github.com/${site.repo}/blob/main/LICENSE`,
        sameAs: `https://github.com/${site.repo}`,
        relatedLink,
        featureList: [
          'Structured JSON and YAML output for coding agents',
          'Read-only safety mode',
          'Non-interactive automation flags',
          'Works with any coding agent or agent harness that can run shell commands',
        ],
        keywords: [
          'coding agent',
          'AI agent CLI',
          'agent harness',
          'MCP-free shell integration',
          'terminal automation',
          'Google Search Console automation',
          'Search Console API CLI',
        ],
        offers: {
          '@type': 'Offer',
          price: '0',
          priceCurrency: 'USD',
        },
      },
      {
        '@type': 'WebSite',
        name: site.name,
        url: siteUrl,
        description: site.description,
        sameAs: `https://github.com/${site.repo}`,
        relatedLink,
      },
    ],
  };

  return (
    <main className="osmo-home flex flex-1 flex-col">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(structuredData).replace(/</g, '\\u003c'),
        }}
      />
      <HomeHero />

      {/* Stack strip */}
      {site.compatible && site.compatible.length > 0 ? (
        <section className="osmo-section osmo-section--compatible">
          <div className="osmo-container">
            <Reveal className="compatible-marquee">
              <div className="compatible-marquee__track">
                {Array.from(
                  {
                    // Each copy is one full set, so the scroll speed stays fixed
                    // per site; enough copies guarantee the track always overruns
                    // the widest container so no blank gap sweeps through.
                    length: Math.max(
                      4,
                      Math.ceil(24 / site.compatible.length) + 1,
                    ),
                  },
                  (_, copyIndex) => (
                    <span
                      className="compatible-marquee__list"
                      aria-hidden={copyIndex > 0 || undefined}
                      key={copyIndex}
                    >
                      {site.compatible?.map((item) => (
                        <span className="compatible-marquee__item" key={item}>
                          {item}
                          <span aria-hidden>{' · '}</span>
                        </span>
                      ))}
                    </span>
                  ),
                )}
              </div>
            </Reveal>
          </div>
        </section>
      ) : null}

      {/* Features */}
      <section
        className="osmo-section osmo-section--features"
        data-theme-section="dark"
        aria-labelledby="capabilities-heading"
      >
        <div className="osmo-container">
          <Reveal className="osmo-section__header">
            <h2 id="capabilities-heading" className="osmo-section__title">
              {site.featuresTitle ?? 'Everything, from one binary'}
            </h2>
            <p className="osmo-section__subtitle">
              {site.featuresSubtitle ??
                'Built for humans at the keyboard and coding agents alike.'}
            </p>
          </Reveal>

          <div className="osmo-card-grid osmo-card-grid--features">
            {site.features.map(({ title, body, docsLink }, index) => (
              <Reveal
                key={title}
                delay={revealDelays[index % revealDelays.length]}
                className="osmo-card osmo-feature-card"
              >
                <span className="osmo-eyebrow osmo-card__number">
                  {String(index + 1).padStart(2, '0')}
                </span>
                <h3 className="osmo-card__title">{title}</h3>
                <p className="osmo-card__body">
                  <LinkedFeatureBody body={body} docsLink={docsLink} />
                </p>
              </Reveal>
            ))}
          </div>
        </div>
      </section>

      {/* Google data use */}
      <section
        className="osmo-section osmo-section--data-use"
        aria-labelledby="data-use-heading"
      >
        <div className="osmo-container">
          <Reveal className="osmo-section__header">
            <h2 id="data-use-heading" className="osmo-section__title">
              How gsc uses your Google data
            </h2>
            <p className="osmo-section__subtitle">
              gsc runs on your computer and signs in with your Google account to
              reach Search Console on your behalf. Read the{' '}
              <Link href="/privacy">privacy policy</Link> for the details.
            </p>
          </Reveal>

          <div className="osmo-card-grid osmo-card-grid--features">
            {dataUse.map(({ title, body }, index) => (
              <Reveal
                key={title}
                delay={revealDelays[index % revealDelays.length]}
                className="osmo-card osmo-feature-card"
              >
                <h3 className="osmo-card__title">{title}</h3>
                <p className="osmo-card__body">{body}</p>
              </Reveal>
            ))}
          </div>
        </div>
      </section>

      {/* CTA band */}
      <section className="osmo-section osmo-section--cta">
        <div className="osmo-container">
          <Reveal className="osmo-cta-panel">
            <h2 className="osmo-section__title">Ready in one command</h2>
            <p className="osmo-section__subtitle">
              {site.ctaBody ??
                'Install the binary, authenticate, and start querying. No runtime, no dependencies.'}
            </p>
            <div className="osmo-cta-panel__actions">
              <InstallCommand command={site.installCommand} />
              <OsmoButton
                href="/docs"
                aria-label="Read the docs"
                icon={<ArrowRight />}
              >
                Read the docs
              </OsmoButton>
            </div>
          </Reveal>
        </div>
      </section>

      <SiteFooter />
    </main>
  );
}

function LinkedFeatureBody({
  body,
  docsLink,
}: {
  body: string;
  docsLink?: { label: string; href: string };
}) {
  if (!docsLink) return body;

  const linkIndex = body.indexOf(docsLink.label);
  if (linkIndex === -1) return body;

  const before = body.slice(0, linkIndex);
  const after = body.slice(linkIndex + docsLink.label.length);

  return (
    <>
      {before}
      <Link href={docsLink.href}>{docsLink.label}</Link>
      {after}
    </>
  );
}
