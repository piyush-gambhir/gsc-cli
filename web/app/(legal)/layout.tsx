import { FloatingHeader } from '@/components/floating-header';
import { SiteFooter } from '@/components/site-footer';

export default function LegalLayout({ children }: LayoutProps<'/'>) {
  return (
    <div className="marketing-shell legal-shell">
      <FloatingHeader />
      <main className="legal-shell__main">{children}</main>
      <SiteFooter />
    </div>
  );
}
