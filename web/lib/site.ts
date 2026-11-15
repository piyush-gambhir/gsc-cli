import {
  Bot,
  ChartLine,
  FolderDown,
  KeyRound,
  ScanSearch,
  SlidersHorizontal,
  type LucideIcon,
} from 'lucide-react';

export interface Feature {
  icon: LucideIcon;
  title: string;
  body: string;
  docsLink?: {
    label: string;
    href: string;
  };
}

export interface SiteConfig {
  name: string;
  binary: string;
  repo: string;
  tagline: string;
  description: string;
  badge: string;
  installCommand: string;
  features: Feature[];
  exampleTitle: string;
  example: string;
  compatible?: string[];
  featuresTitle?: string;
  featuresSubtitle?: string;
  ctaBody?: string;
  accent?: string;
  accentName?: string;
  accentHex?: string;
}

export const site: SiteConfig = {
  name: 'gsc-cli',
  binary: 'gsc',
  repo: 'piyush-gambhir/gsc-cli',
  tagline: 'Google Search Console from your terminal',
  description:
    'gsc-cli is an independent, unofficial open-source CLI for Google Search Console. Sign in with your Google account to read search performance, see how Google indexed your pages, manage sitemaps and properties, and export your data, from a scriptable tool built for people and coding agents alike.',
  badge: 'Open-source · Agent-friendly',
  accent: 'oklch(0.72 0.13 265)',
  accentName: 'periwinkle',
  accentHex: '#7ca2f6',
  installCommand:
    'curl -fsSL https://raw.githubusercontent.com/piyush-gambhir/gsc-cli/main/install.sh | sh',
  features: [
    {
      icon: ChartLine,
      title: 'Search performance',
      body: 'Totals that match the Performance report, top queries and pages, trends by hour, day, week, or month, and period comparisons that never count a missing row as zero.',
      docsLink: {
        label: 'period comparisons',
        href: '/docs/commands/performance',
      },
    },
    {
      icon: SlidersHorizontal,
      title: 'Every query option',
      body: 'All seven dimensions, six search types, regex filters, hourly data, and every row Google exposes, well past the web UI’s 1,000-row table.',
      docsLink: {
        label: 'every row Google exposes',
        href: '/docs/commands/query',
      },
    },
    {
      icon: ScanSearch,
      title: 'URL Inspection',
      body: 'Google’s indexed view of a page: verdict, coverage, last crawl, canonicals, and rich results, for one URL or a paced batch that stops at the quota.',
      docsLink: {
        label: 'a paced batch',
        href: '/docs/commands/inspection',
      },
    },
    {
      icon: KeyRound,
      title: 'One-command login',
      body: 'gsc auth login opens your browser once and keeps the token in your OS keychain. Service accounts, ADC, and impersonation cover CI.',
      docsLink: {
        label: 'Service accounts, ADC, and impersonation',
        href: '/docs/authentication',
      },
    },
    {
      icon: Bot,
      title: 'Built for agents',
      body: '-o json envelopes that state dates, data state, and completeness, structured errors, --read-only, and --dry-run for every write.',
      docsLink: {
        label: 'structured errors',
        href: '/docs/agents',
      },
    },
    {
      icon: FolderDown,
      title: 'Export & insights',
      body: 'Resumable day-by-day export to CSV or NDJSON, plus striking-distance, low-CTR, and cannibalization reports computed locally with their thresholds shown.',
      docsLink: {
        label: 'Resumable day-by-day export',
        href: '/docs/commands/export',
      },
    },
  ],
  exampleTitle: 'An eight-line tour',
  example: `# Sign in once; tokens refresh automatically
gsc auth login
# Totals against the previous period, then top queries
gsc performance --compare previous -o json
gsc top queries --limit 20 -o json
# Google's indexed view of a page
gsc inspect https://www.example.com/pricing
# Queries ranking just off the top
gsc insights striking-distance -o json`,
  compatible: [
    'Search Analytics',
    'URL Inspection',
    'Sitemaps',
    'Properties',
    'Hourly data',
    'Period comparison',
    'Bulk export',
    'Insights',
  ],
  ctaBody:
    'Install the binary, sign in with Google once, and start reading your Search Console data. No runtime, no dependencies.',
};
