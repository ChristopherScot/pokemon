import { defineConfig } from 'vite'

export default defineConfig({
  // The pokedex client is a file: dependency (symlink); preserving it here
  // lets Rollup resolve its transitive deps through the workspace root.
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
