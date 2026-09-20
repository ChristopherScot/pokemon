// Where each page's browser bundle lives.
//
// Vite emits hashed filenames - battle-yuBHzGeE.js - and writes a
// manifest mapping the entry to whatever it produced. The server reads
// that manifest so the <script src> it emits always matches the file
// that was built. Hashing is what makes caching safe: a deploy changes
// the URL, so no browser can serve a stale bundle, and the assets can
// be cached hard.
//
// Read once at startup. The manifest does not change while the process
// runs, and per-request disk IO does not belong on the hot path.
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))

// dist/assets when running the built server (which sits in dist/ beside
// them), ./dist/assets when running the sources directly. Resolved from
// this file, so neither depends on the working directory - and picked
// once, so the manifest and the file route cannot disagree about where
// the assets are.
function findAssetDir(): string {
  for (const dir of [join(here, 'assets'), join(here, 'dist', 'assets')]) {
    try {
      readFileSync(join(dir, '.vite', 'manifest.json'))
      return dir
    } catch {
      // try the next location
    }
  }
  throw new Error(
    'no asset manifest - run `npm run build:client` before starting the server',
  )
}

/** Where the built assets are. */
export const ASSET_DIR = findAssetDir()

type Manifest = Record<string, { file: string; isEntry?: boolean; imports?: string[] }>

let manifest: Manifest | null = null

function load(): Manifest {
  if (!manifest) {
    manifest = JSON.parse(
      readFileSync(join(ASSET_DIR, '.vite', 'manifest.json'), 'utf8'),
    ) as Manifest
  }
  return manifest
}

/**
 * The URL for a page's bundle, e.g. "/assets/battle-yuBHzGeE.js".
 *
 * Throws rather than returning a guess: a page that ships a <script>
 * pointing at nothing renders, looks fine, and does nothing - which is
 * the failure mode this whole file exists to make impossible.
 */
export function assetURL(entry: Entry): string {
  return `/assets/${lookup(entry).file}`
}

export type Entry = 'battle' | 'lobby' | 'pokedex'

function lookup(entry: Entry) {
  const hit = load()[`client/${entry}-entry.tsx`]
  if (!hit) throw new Error(`no built bundle for ${entry}`)
  return hit
}

/**
 * The chunks a page's entry imports, for <link rel="modulepreload">.
 *
 * Once more than one page shares React, Rollup splits it into a chunk
 * the entry imports rather than inlining it in both. The browser would
 * fetch that chunk anyway when it parses the entry's import - but only
 * after parsing it, which is a wasted round trip on the critical path.
 * Preloading is what closes it.
 */
export function preloadURLs(entry: Entry): string[] {
  const m = load()
  const out: string[] = []
  const seen = new Set<string>()

  // Transitively, not just the direct imports. Rollup splits shared
  // code as it sees fit - today React is one chunk and the type palette
  // another - and a chunk that imports a chunk is entirely normal. A
  // preload list that stopped at depth one would silently miss those
  // the day the graph got deeper.
  const walk = (key: string) => {
    if (seen.has(key)) return
    seen.add(key)
    const chunk = m[key]
    if (!chunk) return
    out.push(`/assets/${chunk.file}`)
    for (const next of chunk.imports ?? []) walk(next)
  }
  for (const key of lookup(entry).imports ?? []) walk(key)
  return out
}
