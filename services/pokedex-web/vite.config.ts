// The production build: one file, no node_modules.
//
// Locally this service runs `node server.ts` directly - Node strips the
// types, nothing is compiled, and the edit-run loop has no build step.
// The image runs a bundle instead, for one reason: inlining the
// dependencies means the container needs no node_modules, and THAT is
// what lets this service depend on a generated client in a sibling
// directory via `file:`. A symlinked sibling cannot be copied into an
// image; an inlined one does not need to be.
//
// The two paths are a real cost - what you run locally is not byte-wise
// what ships - so CI runs the BUNDLE, not just builds it.
import { readFileSync } from 'node:fs'

import { defineConfig } from 'vite'

// The browser bundles, read at BUILD time and frozen into the server
// bundle as string constants.
//
// Not read at runtime. The image copies dist/server.js and nothing
// else - that is the whole "one file, no node_modules" deployment - so
// a server that read dist/client/*.js on demand started fine, passed
// CI (which runs from the repo, where those files still exist), and
// returned 500 for every page in the image. Inlining here is what
// makes the single-file claim true.
function inlineClientBundles() {
  const names = ['battle']
  const entries = names.map((n) => {
    const src = readFileSync(`dist/client/${n}.js`, 'utf8')
    return [n, src] as const
  })
  return {
    name: 'inline-client-bundles',
    enforce: 'pre' as const,
    resolveId(id: string) {
      return id.endsWith('clientbundle.ts') ? '\0clientbundles' : null
    },
    load(id: string) {
      if (id !== '\0clientbundles') return null
      const map = Object.fromEntries(entries)
      return `const BUNDLES = ${JSON.stringify(map)}
export function clientBundle(name) {
  const src = BUNDLES[name]
  if (src === undefined) throw new Error('no browser bundle ' + name)
  return src
}`
    },
  }
}

export default defineConfig({
  plugins: [inlineClientBundles()],
  build: {
    ssr: true,
    // Matches the distroless runtime, so Vite does not downlevel syntax
    // the shipped Node can run natively.
    target: 'node22',
    outDir: 'dist',
    // Do NOT empty dist/: the browser bundles are built into
    // dist/client first, and Vite's default would delete them here -
    // leaving a server that starts, serves a page, and throws on the
    // first request because the script it inlines is gone.
    emptyOutDir: false,
    // The bundle is one file and nothing reads it but Node; a sourcemap
    // would double the image's only layer for a stack trace that still
    // points at inlined code.
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
    // Everything else is inlined. Without this Vite externalises
    // dependencies, the bundle is a few hundred bytes of imports, and
    // the image needs node_modules after all.
    noExternal: true,
  },
})
