import { defineConfig } from 'vite'

export default defineConfig({
  // The pokedex client is a file: dependency, so node_modules holds a
  // SYMLINK to ../pokedex/clients/ts. Without this, Rollup resolves the
  // client's own imports from that real path, walks up looking for
  // node_modules, finds none, and fails on "openapi-fetch".
  //
  // Node resolves the same way for the same reason - it realpaths by
  // default - which is why package.json runs server.ts with
  // --preserve-symlinks. Both the bundler and the runtime need telling.
  resolve: {
    preserveSymlinks: true,
  },
  build: {
    ssr: true,
    target: 'node22',
    outDir: 'dist',
    emptyOutDir: false,
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
    noExternal: true,
  },
})
