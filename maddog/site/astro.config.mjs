import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// Served from the custom domain maddog.dev at the site root.
export default defineConfig({
  site: 'https://sekkit.github.io',
  base: '/maddog',
  build: { assets: 'static' },
  integrations: [sitemap()],
});
