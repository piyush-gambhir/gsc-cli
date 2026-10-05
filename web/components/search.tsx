'use client';
import {
  SearchDialog,
  SearchDialogClose,
  SearchDialogContent,
  SearchDialogHeader,
  SearchDialogIcon,
  SearchDialogInput,
  SearchDialogList,
  SearchDialogOverlay,
  type SharedProps,
} from 'fumadocs-ui/components/dialog/search';
import { useDocsSearch } from 'fumadocs-core/search/client';
import { staticClient } from 'fumadocs-core/search/client/orama-static';
import { useI18n } from 'fumadocs-ui/contexts/i18n';

export default function DefaultSearchDialog(props: SharedProps) {
  const { locale } = useI18n(); // (optional) for i18n
  const { search, setSearch, query } = useDocsSearch({
    // Fumadocs defaults to /api/search, which misses Next's basePath.
    client: staticClient({
      from: `${process.env.NEXT_PUBLIC_BASE_PATH ?? ''}/api/search`,
      locale,
    }),
  });

  return (
    <SearchDialog search={search} onSearchChange={setSearch} isLoading={query.isLoading} {...props}>
      <SearchDialogOverlay className="search-dialog__overlay" />
      <SearchDialogContent className="search-dialog__content">
        <SearchDialogHeader className="search-dialog__header">
          <SearchDialogIcon />
          <SearchDialogInput className="search-dialog__input" />
          <SearchDialogClose className="search-dialog__close" />
        </SearchDialogHeader>
        <SearchDialogList
          className="search-dialog__list"
          items={query.data !== 'empty' ? query.data : null}
        />
      </SearchDialogContent>
    </SearchDialog>
  );
}
