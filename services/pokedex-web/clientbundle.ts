// Reads the browser bundles built by vite.client.config.ts.
//
// The pages inline their script rather than linking it. That is the
// same delivery as before this code became real modules - one
// self-contained document, no static routes, no cache-busting, and an
// image that is still a single file with no node_modules. What changed
// is that the SOURCE is TypeScript the compiler checks and the tests
// import, instead of a template literal nothing could see into.
//
// Read once at startup: the files do not change while the process runs,
// and a per-request read would put disk IO on the hot path.
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))

// dist/client when running the built server, ./dist/client when running
// the sources directly. Both are resolved from this file, so neither
// depends on the process's working directory.
const candidates = (name: string) => [
  join(here, 'client', `${name}.js`),
  join(here, 'dist', 'client', `${name}.js`),
]

const cache = new Map<string, string>()

/**
 * The built browser bundle for a page, ready to inline.
 *
 * Throws rather than returning empty if it is missing: a page that
 * serves an empty <script> looks fine and does nothing, which is the
 * failure mode this whole change exists to stop happening quietly.
 */
export function clientBundle(name: string): string {
  const hit = cache.get(name)
  if (hit !== undefined) return hit

  for (const path of candidates(name)) {
    try {
      const src = readFileSync(path, 'utf8')
      cache.set(name, src)
      return src
    } catch {
      // try the next location
    }
  }
  throw new Error(
    `browser bundle ${name}.js not found - run \`npm run build:client\` ` +
      `(looked in ${candidates(name).join(', ')})`,
  )
}
