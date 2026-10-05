# gsc-cli docs site

The site at [projects.piyushgambhir.com/gsc-cli](https://projects.piyushgambhir.com/gsc-cli):
Next.js with [Fumadocs](https://fumadocs.dev), exported as static files and served by a Cloudflare Worker.

```bash
pnpm install
pnpm dev                 # http://localhost:3000/gsc-cli
pnpm types:check
pnpm build:cloudflare    # static export into .cloudflare/assets/gsc-cli
pnpm test:search         # checks the exported search index
```

- Guide pages: `content/docs/`. Site name, hero, features, and accent: `lib/site.ts`.
- `content/docs/reference/` is generated from the repository's `docs/` by `scripts/sync-reference.mjs`,
  which every `dev`, `build`, and `types:check` runs first. Edit `docs/` (or the Go command help) instead.
- `lib/suite.ts` lists the sibling project sites linked from `llms.txt` and the home page metadata.
- Deploy with `../scripts/deploy-docs.sh` after `wrangler login`.
