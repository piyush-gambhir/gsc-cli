import { source } from '@/lib/source';
import { llms } from 'fumadocs-core/source';
import { site } from '@/lib/site';
import { siteUrl } from '@/lib/shared';
import { getOtherSuiteProjects } from '@/lib/suite';

export const revalidate = false;

export async function GET() {
  // index() returns a Promise since fumadocs-core 16.15.17.
  const index = (await llms(source).index()).replace(/\]\((\/[^)]+)\)/g, (_match, path: string) => `](${siteUrl}${path})`);
  const relatedSites = getOtherSuiteProjects(site.repo)
    .map(({ name, href }) => `- ${name}: ${href}`)
    .join('\n');
  const intro =
    'gsc-cli (binary: gsc) is an independent, unofficial command-line interface for Google Search Console, built for people and coding agents. It covers all 10 active Search Console API methods: Search Analytics with every option, URL Inspection, sitemaps, and properties, plus period comparisons, resumable bulk export, and locally computed insights. Agents should run commands with -o json --no-input, use --read-only unless asked to change something, and preview writes with --dry-run. Run gsc freshness before trusting the last few days, take totals from gsc performance instead of summing query or page rows, and treat gsc inspect as Google\'s indexed view, not a live test.';
  return new Response(
    `${intro}\n\n${index}\n\n## Related CLI sites\n\n${relatedSites}\n`,
  );
}
