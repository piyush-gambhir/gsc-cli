import type { ReactNode } from 'react';

interface LegalPageProps {
  title: string;
  /** Date the page's current text took effect; update it when the text changes. */
  effective?: string;
  children: ReactNode;
}

export function LegalPage({ title, effective = 'October 5, 2026', children }: LegalPageProps) {
  return (
    <article className="legal-page">
      <header className="legal-page__header">
        <h1>{title}</h1>
        <p className="legal-page__effective">Effective {effective}</p>
      </header>
      <div className="legal-page__content">{children}</div>
    </article>
  );
}
