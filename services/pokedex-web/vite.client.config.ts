// The BROWSER build: React, bundled per page, emitted with hashed
// filenames into dist/assets.
//
// Separate from vite.config.ts because the two targets agree about
// nothing - that one is ssr/node22/one-file, this is a browser bundle
// the page loads with <script src>.
//
// Hashed names are what makes caching safe: the server reads the
// manifest at build time and emits whatever filename this produced, so
// a deploy changes the URL and no browser can serve a stale bundle.
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist/assets',
    emptyOutDir: true,
    // The browsers this runs in are modern; there is no build-for-IE
    // story here and downlevelling would only make the bundle bigger.
    target: 'es2022',
    sourcemap: false,
    // The server needs to know the hashed filenames it should emit.
    manifest: true,
    rollupOptions: {
      input: {
        battle: 'client/battle-entry.tsx',
        lobby: 'client/lobby-entry.tsx',
        pokedex: 'client/pokedex-entry.tsx',
      },
      output: {
        format: 'esm',
        entryFileNames: '[name]-[hash].js',
        chunkFileNames: '[name]-[hash].js',
        assetFileNames: '[name]-[hash][extname]',
      },
    },
  },
})
