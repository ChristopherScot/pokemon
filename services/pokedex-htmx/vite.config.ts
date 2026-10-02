// Production build: one file, no node_modules. Locally the service runs
// `node server.ts` directly; the image runs a bundle because inlining
// dependencies lets this service depend on a generated client in a
// sibling directory via `file:` - a symlinked sibling cannot be copied
// into an image, an inlined one does not need to be.
//
// The two paths are a real cost, so CI runs the BUNDLE, not just builds it.
import { defineConfig } from 'vite'

export default defineConfig({
  // The pokedex client is a file: dependency, so node_modules holds a
  // symlink to ../pokedex/clients/ts. Without this, Rollup resolves the
  // client's own imports from the real path, walks up looking for
  // node_modules, finds none, and fails on "openapi-fetch". Node needs
  // the same via --preserve-symlinks in package.json's scripts.
  resolve: {
    preserveSymlinks: true,
  },
  build: {
    ssr: true,
    // Matches the distroless runtime so Vite does not downlevel syntax.
    target: 'node22',
    outDir: 'dist',
    // Sourcemap doubles the image's only layer for a trace pointing at
    // inlined code.
    sourcemap: false,
    rollupOptions: {
      input: 'server.ts',
      output: {
        format: 'esm',
        entryFileNames: 'server.js',
        inlineDynamicImports: true,
      },
      // node: builtins stay external - they ARE the runtime.
      external: [/^node:/],
    },
  },
  ssr: {
    // Without this Vite externalises dependencies and the image needs
    // node_modules after all.
    noExternal: true,
  },
})
