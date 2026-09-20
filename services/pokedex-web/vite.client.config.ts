// The BROWSER bundle, separate from the server build in vite.config.ts.
//
// Two configs because the two targets disagree about everything: the
// server is ssr/node22/one-file, this is a browser/esm/per-page bundle.
//
// The output is inlined into the page's <script> tag at boot rather
// than served as a static asset. That keeps the deployment exactly as
// it was - one file, no node_modules, no static routes, no cache
// busting - while the SOURCE is ordinary TypeScript the compiler
// checks and the tests import.
import { defineConfig } from 'vite'

export default defineConfig({
  build: {
    outDir: 'dist/client',
    emptyOutDir: true,
    // The browsers this has to run in are the ones with import maps and
    // top-level await; there is no build-for-IE story here.
    target: 'es2022',
    sourcemap: false,
    rollupOptions: {
      input: {
        battle: 'client/battle.ts',
      },
      output: {
        format: 'esm',
        entryFileNames: '[name].js',
        // One file per page, so a page inlines exactly its own code.
        inlineDynamicImports: false,
        manualChunks: undefined,
      },
    },
  },
})
