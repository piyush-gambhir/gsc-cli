export const appName = 'gsc-cli';
export const siteUrl = 'https://projects.piyushgambhir.com/gsc-cli';
export const docsRoute = '/docs';
export const docsImageRoute = '/og/docs';
export const docsContentRoute = '/llms.mdx/docs';

const codeOrRootLink =
  /(?<![\\`])(`+)(?!`)[\s\S]*?(?<!`)\1(?!`)|(?:\]\(<?|href="|^[ \t]*\[(?!\^)[^\]\n]+\]:[ \t]*<?)(?=\/(?!\/))/gm;

export function absoluteLinks(markdown: string): string {
  return markdown.replace(codeOrRootLink, (match, ticks?: string) => (ticks ? match : match + siteUrl));
}

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
