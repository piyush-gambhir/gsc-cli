// Verify the built agent Markdown (llms.txt, llms-full.txt, per-page content.md)
// links only to absolute URLs: agents read these files outside the site, where
// root-relative links (/docs/...) resolve against the domain root and miss the
// basePath, and unevaluated JSX (href={...}) is not a link at all.
import assert from 'node:assert/strict';
import { readdir, readFile } from 'node:fs/promises';
import { siteUrl } from '../lib/shared.ts';

const out = new URL('../out/', import.meta.url);
const pages = (await readdir(new URL('llms.mdx/', out), { recursive: true }))
  .filter((f) => f.endsWith('.md'))
  .map((f) => `llms.mdx/${f}`);
assert.ok(pages.length > 0, 'no per-page Markdown under out/llms.mdx');
// Code examples are not links: blank out code spans and fences (a run of N
// unescaped backticks up to the next run of exactly N), keeping line breaks.
const code = /(?<![\\`])(`+)(?!`)[\s\S]*?(?<!`)\1(?!`)/g;
// Inline links, <a>/<Card> hrefs, and reference definitions ([x]: /path),
// with optional <...> around the destination.
const link = /\]\(<?([^)\s>]*)|href="([^"]*)"|^[ \t]*\[(?!\^)[^\]\n]+\]:[ \t]*<?([^\s>]*)/gm;
for (const file of ['llms.txt', 'llms-full.txt', ...pages]) {
  const text = (await readFile(new URL(file, out), 'utf8')).replace(code, (m) => m.replace(/[^\n]/g, ' '));
  for (const [match, md, href, ref] of text.matchAll(link)) {
    const target = md ?? href ?? ref;
    assert.ok(/^(https?:\/\/|mailto:|#)/.test(target), `${file} has a non-absolute link: ${match.trim()}`);
  }
}
// A sidebar separator followed by a folder of the same name lists the section
// twice in the llms.txt index ("- **Commands**" then "- Commands").
const index = await readFile(new URL('llms.txt', out), 'utf8');
const labels = [...index.matchAll(/^[ \t]*- (?:\*\*)?([^[*\n]+?)(?:\*\*)?$/gm)].map((m) => m[1]);
assert.equal(new Set(labels).size, labels.length, `llms.txt lists a section twice: ${labels.join(', ')}`);
const full = await readFile(new URL('llms-full.txt', out), 'utf8');
assert.ok(full.includes(`${siteUrl}/docs/`), 'llms-full.txt has no absolute docs links');
console.log(`Agent Markdown links are absolute in ${pages.length + 2} files`);
