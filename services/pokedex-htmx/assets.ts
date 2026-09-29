// vendor/sources.ts is generated because disk reads break in the distroless image
// and Vite's `?raw` breaks under plain `node server.ts`.
import { createHash } from 'node:crypto'

import { HTMX as HTMX_SOURCE, IDIOMORPH_EXT as MORPH_SOURCE, PAGE_JS as PAGE_SOURCE } from './vendor/sources.ts'

// Content-addressed so `immutable` cache-control is honest across version bumps.
const digest = (s: string) => createHash('sha256').update(s).digest('hex').slice(0, 8)

export type Asset = { url: string; body: string }

const asset = (name: string, body: string): Asset =>
  ({ url: `/assets/${name}-${digest(body)}.js`, body })

export const HTMX = asset('htmx', HTMX_SOURCE)
export const MORPH = asset('idiomorph-ext', MORPH_SOURCE)
export const PAGE_JS = asset('page', PAGE_SOURCE)

export const byURL = new Map([HTMX, MORPH, PAGE_JS].map((a) => [a.url, a]))
