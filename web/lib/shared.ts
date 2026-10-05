export const appName = 'Search Console CLI';
export const siteUrl = 'https://projects.piyushgambhir.com/gsc-cli';
export const docsRoute = '/docs';
export const docsImageRoute = '/og/docs';
export const docsContentRoute = '/llms.mdx/docs';

// The tracked file behind a docs page: guides live in web/content/docs, and the
// reference pages are generated from docs/ (see scripts/sync-reference.mjs).
const generatedSources: Record<string, string> = {
  'reference/commands.mdx': 'docs/commands.md',
  'reference/api-coverage.mdx': 'docs/api-coverage.md',
};

export function sourcePath(pagePath: string): string {
  return generatedSources[pagePath] ?? `web/content/docs/${pagePath}`;
}

export const gitConfig = {
  user: 'piyush-gambhir',
  repo: 'gsc-cli',
  branch: 'main',
};
