// htmx and the morph extension, carried in the bundle as strings.
//
// vendor/sources.ts is GENERATED from the vendored .js files, because
// neither obvious alternative works on both paths this service runs on:
// reading from disk breaks in the image (the Dockerfile copies
// dist/server.js and nothing else), and Vite's `?raw` breaks under
// plain `node server.ts`, which is how it runs locally and in tests.
import { createHash } from 'node:crypto'

import { HTMX as HTMX_SOURCE, IDIOMORPH_EXT as MORPH_SOURCE, PAGE_JS as PAGE_SOURCE } from './vendor/sources.ts'

// Content-addressed, so `immutable` is honest: a version bump changes
// the URL rather than serving a year-stale file from a browser cache.
const digest = (s: string) => createHash('sha256').update(s).digest('hex').slice(0, 8)

export type Asset = { url: string; body: string }

const asset = (name: string, body: string): Asset =>
  ({ url: `/assets/${name}-${digest(body)}.js`, body })

export const HTMX = asset('htmx', HTMX_SOURCE)
export const MORPH = asset('idiomorph-ext', MORPH_SOURCE)
export const PAGE_JS = asset('page', PAGE_SOURCE)

export const byURL = new Map([HTMX, MORPH, PAGE_JS].map((a) => [a.url, a]))
