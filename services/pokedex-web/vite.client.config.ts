import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  // Same reason as vite.config.ts: the client package is a symlinked
  // file: dependency, and the bundler must resolve through the link to
  // find the hoisted node_modules.
  resolve: {
    preserveSymlinks: true,
  },
  plugins: [react()],
  build: {
    outDir: 'dist/assets',
    emptyOutDir: true,
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
