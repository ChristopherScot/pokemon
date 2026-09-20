import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))

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

export function assetURL(entry: Entry): string {
  return `/assets/${lookup(entry).file}`
}

export type Entry = 'battle' | 'lobby' | 'pokedex'

function lookup(entry: Entry) {
  const hit = load()[`client/${entry}-entry.tsx`]
  if (!hit) throw new Error(`no built bundle for ${entry}`)
  return hit
}

export function preloadURLs(entry: Entry): string[] {
  const m = load()
  const out: string[] = []
  const seen = new Set<string>()

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
