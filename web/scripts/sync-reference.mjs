// Generates the reference pages from the repository's docs/ (written by `make docs`),
// so the site always matches the CLI. The output is gitignored; builds run this first.
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

const root = path.resolve(import.meta.dirname, '..');
const blob = 'https://github.com/piyush-gambhir/gsc-cli/blob/main';
const outDir = path.join(root, 'content/docs/reference');

// Repository docs that have their own page on this site, with the link text to show.
const siteLinks = {
  'docs/api-coverage.md': ['/docs/reference/api-coverage', 'API coverage'],
  'docs/auth.md': ['/docs/authentication', 'Authentication'],
  'docs/commands.md': ['/docs/reference/commands', 'Command reference'],
  'docs/compatibility.md': ['/docs/compatibility', 'Compatibility'],
};

// `generated` matches a generated-file header to replace with `intro`; pages
// without one (api-coverage.md is maintained by hand and checked by
// TestAPICoverage) are copied as they are.
const pages = [
  {
    from: 'docs/commands.md',
    slug: 'commands',
    title: 'Command reference',
    description: 'Every gsc command with its flags and examples, generated from the command tree.',
    generated: /^Generated from the command tree\. Run `make docs` to refresh\.\n/,
    intro: 'Generated from the `gsc` command tree on `main`. The [command guides](/docs/commands) explain when to use each group.\n',
  },
  {
    from: 'docs/api-coverage.md',
    slug: 'api-coverage',
    title: 'API coverage',
    description: 'Every Search Console API method with the gsc commands that call it, and the methods left out with the reason.',
  },
];

// Points a relative link at this site's page for that doc, or at the file on GitHub.
function rewriteLink(text, target, fromFile) {
  if (/^[a-z]+:/i.test(target) || target.startsWith('#') || target.startsWith('/')) return `[${text}](${target})`;
  const [file, hash = ''] = target.split('#');
  const resolved = path.posix.normalize(path.posix.join(path.posix.dirname(fromFile), file));
  const anchor = hash ? `#${hash}` : '';
  const page = siteLinks[resolved];
  if (!page) return `[${text}](${blob}/${resolved}${anchor})`;
  return `[${text.endsWith('.md') ? page[1] : text}](${page[0]}${anchor})`;
}

// MDX treats { } and < as syntax in prose; code spans and fences stay literal.
function escapeProse(text) {
  return text.replace(/[{}<]/g, (c) => (c === '<' ? '&lt;' : `\\${c}`));
}

function convert(markdown, fromFile) {
  const out = [];
  let fenced = false;
  for (const line of markdown.split('\n')) {
    if (/^\s*```/.test(line)) {
      fenced = !fenced;
      out.push(line);
      continue;
    }
    if (fenced) {
      out.push(line);
      continue;
    }
    const linked = line.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, text, target) => rewriteLink(text, target, fromFile));
    out.push(linked.split(/(`[^`]*`)/).map((part, i) => (i % 2 ? part : escapeProse(part))).join(''));
  }
  return out.join('\n');
}

await mkdir(outDir, { recursive: true });
for (const page of pages) {
  let body = await readFile(path.join(root, '..', page.from), 'utf8');
  body = body.replace(/^# .*\n+/, '');
  if (page.generated) {
    if (!page.generated.test(body)) throw new Error(`${page.from}: generated header changed; update sync-reference.mjs`);
    body = body.replace(page.generated, page.intro);
  }
  const frontmatter = `---\ntitle: ${JSON.stringify(page.title)}\ndescription: ${JSON.stringify(page.description)}\n---\n\n`;
  await writeFile(path.join(outDir, `${page.slug}.mdx`), frontmatter + convert(body, page.from));
}
await writeFile(
  path.join(outDir, 'meta.json'),
  `${JSON.stringify({ title: 'Reference', pages: pages.map((p) => p.slug) }, null, 2)}\n`,
);
console.log(`Synced ${pages.length} reference pages from docs/`);
