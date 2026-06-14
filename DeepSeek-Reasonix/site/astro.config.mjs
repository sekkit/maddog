import { defineConfig } from 'astro/config';

// Served from GitHub Pages under the repo subpath.
export default defineConfig({
  site: 'https://sekkit.github.io',
  base: '/maddog',
  build: { assets: 'static' },
});
