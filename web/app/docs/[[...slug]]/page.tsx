import { getPageImage, getPageMarkdownUrl, source } from '@/lib/source';
import {
  DocsBody,
  DocsDescription,
  DocsPage,
  DocsTitle,
  MarkdownCopyButton,
  ViewOptionsPopover,
} from 'fumadocs-ui/layouts/docs/page';
import { notFound } from 'next/navigation';
import { getMDXComponents } from '@/components/mdx';
import type { Metadata } from 'next';
import { createRelativeLink } from 'fumadocs-ui/mdx';
import { gitConfig, sourcePath } from '@/lib/shared';
import { createPageMetadata, siteMetadataDescription } from '@/lib/metadata';
import { site } from '@/lib/site';
import { siteUrl } from '@/lib/shared';

function getMetadataDescription(title: string) {
  return `${title}: independent, unofficial Google Search Console CLI (gsc) for people and coding agents, with JSON output, read-only safety, and dry runs.`;
}

export default async function Page(props: PageProps<'/docs/[[...slug]]'>) {
  const params = await props.params;
  const page = source.getPage(params.slug);
  if (!page) notFound();

  const MDX = page.data.body;
  const markdownUrl = `${siteUrl}${getPageMarkdownUrl(page).url}`;
  const pageUrl = `${siteUrl}${page.url}`;
  const breadcrumbs = [
    { name: 'Home', item: siteUrl },
    { name: 'Documentation', item: `${siteUrl}/docs` },
    ...page.slugs.map((slug, index) => ({
      name: slug
        .split('-')
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(' '),
      item: `${siteUrl}/docs/${page.slugs.slice(0, index + 1).join('/')}`,
    })),
  ].filter((crumb, index, items) => index === 0 || crumb.item !== items[index - 1]?.item);
  const structuredData = {
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'BreadcrumbList',
        itemListElement: breadcrumbs.map((crumb, index) => ({
          '@type': 'ListItem',
          position: index + 1,
          name: crumb.name,
          item: crumb.item,
        })),
      },
      {
        '@type': 'TechArticle',
        headline: page.data.title,
        description: page.data.description,
        url: pageUrl,
        mainEntityOfPage: pageUrl,
        isPartOf: `${siteUrl}/docs`,
        author: {
          '@type': 'Person',
          name: 'Piyush Gambhir',
        },
        publisher: {
          '@type': 'Organization',
          name: site.name,
          url: siteUrl,
        },
      },
    ],
  };

  return (
    <DocsPage toc={page.data.toc} full={page.data.full}>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(structuredData).replace(/</g, '\\u003c'),
        }}
      />
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription className="mb-0">{page.data.description}</DocsDescription>
      <div className="flex flex-row gap-2 items-center pb-6">
        <MarkdownCopyButton markdownUrl={markdownUrl} />
        <ViewOptionsPopover
          markdownUrl={markdownUrl}
          githubUrl={`https://github.com/${gitConfig.user}/${gitConfig.repo}/blob/${gitConfig.branch}/${sourcePath(page.path)}`}
        />
      </div>
      <DocsBody>
        <MDX
          components={getMDXComponents({
            // this allows you to link to other pages with relative file paths
            a: createRelativeLink(source, page),
          })}
        />
      </DocsBody>
    </DocsPage>
  );
}

export async function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata(props: PageProps<'/docs/[[...slug]]'>): Promise<Metadata> {
  const params = await props.params;
  const page = source.getPage(params.slug);
  if (!page) notFound();
  const metadataDescription = getMetadataDescription(page.data.title);

  return createPageMetadata({
    title: page.data.title,
    description: metadataDescription,
    socialDescription: siteMetadataDescription,
    path: page.url,
    image: getPageImage(page).url,
    type: 'article',
  });
}
