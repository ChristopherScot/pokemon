// A DOM small enough to run the page modules under `node --test`.
//
// Not a browser and not trying to be. The client modules touch a
// handful of DOM APIs; this provides those and records what was written
// so a test can assert on it. It exists because the alternative was
// jsdom - a large dependency for a service whose whole deployment story
// is "one file, no node_modules".
//
// The thing it replaces is worse than a fake DOM: the tests used to
// regex the <script> tag out of the rendered HTML and eval it, so they
// could only reach what the string happened to expose, and they passed
// whether or not the code they were testing could even be imported.

export type FakeEl = {
  id: string
  innerHTML: string
  textContent: string
  className: string
  scrollTop: number
  scrollHeight: number
  disabled: boolean
  hidden: boolean
  style: Record<string, string>
  dataset: Record<string, string>
  classList: { add(c: string): void; remove(c: string): void; toggle(c: string, on?: boolean): void }
  children: FakeEl[]
  addEventListener(type: string, fn: () => unknown): void
  removeEventListener(): void
  setAttribute(name: string, value: string): void
  getAttribute(name: string): string | null
  appendChild(child: FakeEl): FakeEl
  remove(): void
  querySelector(): FakeEl | null
  querySelectorAll(): FakeEl[]
  getBoundingClientRect(): { height: number; top: number; bottom: number }
  focus(): void
  handlers: Record<string, Array<() => unknown>>
  attrs: Record<string, string>
}

export function makeEl(id = ''): FakeEl {
  const el: FakeEl = {
    id,
    innerHTML: '', textContent: '', className: '',
    scrollTop: 0, scrollHeight: 0, disabled: false, hidden: false,
    style: {}, dataset: {}, children: [],
    handlers: {}, attrs: {},
    classList: {
      add: () => {}, remove: () => {}, toggle: () => {},
    },
    addEventListener(type, fn) {
      ;(el.handlers[type] ||= []).push(fn)
    },
    removeEventListener() {},
    setAttribute(name, value) { el.attrs[name] = value },
    getAttribute(name) { return el.attrs[name] ?? null },
    appendChild(child) { el.children.push(child); return child },
    remove() {},
    querySelector() { return null },
    querySelectorAll() { return [] },
    getBoundingClientRect() { return { height: 86, top: 0, bottom: 86 } },
    focus() {},
  }
  return el
}

export type FakeDom = {
  els: Map<string, FakeEl>
  get(id: string): FakeEl
  fetches: Array<{ url: string; init?: { body?: string; headers?: Record<string, string> } }>
  /** Replies handed to fetch, in order; the last one repeats. */
  replies: Array<{ ok: boolean; status: number; body?: unknown }>
  location: { pathname: string; href: string; reload(): void }
  reloads: number
  timers: number
  frames: Array<() => void>
  /** Runs any requestAnimationFrame callbacks that were queued. */
  flushFrames(): void
  restore(): void
}

/**
 * Installs a fake document/fetch/timers on globalThis and returns the
 * handles a test needs. Call restore() when done.
 */
export function installDom(opts: { pathname?: string; ids?: string[] } = {}): FakeDom {
  const els = new Map<string, FakeEl>()
  for (const id of opts.ids ?? ['board', 'log', 'banner', 'boot']) els.set(id, makeEl(id))

  const dom: FakeDom = {
    els,
    get(id) {
      let e = els.get(id)
      if (!e) { e = makeEl(id); els.set(id, e) }
      return e
    },
    fetches: [],
    replies: [],
    location: {
      pathname: opts.pathname ?? '/battle/abc123',
      href: '',
      reload() { dom.reloads++ },
    },
    reloads: 0,
    timers: 0,
    frames: [],
    flushFrames() {
      const fs = dom.frames.splice(0)
      for (const f of fs) f()
    },
    restore() {
      for (const k of saved.keys()) {
        const v = saved.get(k)
        if (v === undefined) delete (globalThis as Record<string, unknown>)[k]
        else (globalThis as Record<string, unknown>)[k] = v
      }
    },
  }

  const g = globalThis as unknown as Record<string, unknown>
  const saved = new Map<string, unknown>()
  const set = (k: string, v: unknown) => { saved.set(k, g[k]); g[k] = v }

  set('document', {
    getElementById: (id: string) => els.get(id) ?? null,
    querySelector: () => null,
    querySelectorAll: () => [],
    addEventListener: () => {},
    createElement: () => makeEl(),
    documentElement: { style: { setProperty: () => {} } },
    body: makeEl('body'),
    hidden: false,
  })
  set('location', dom.location)
  set('fetch', async (url: string, init?: { body?: string }) => {
    dom.fetches.push({ url, init })
    const r = dom.replies.length
      ? dom.replies[Math.min(dom.fetches.length - 1, dom.replies.length - 1)]
      : { ok: true, status: 200, body: { version: 1, sides: [], log: [] } }
    return { ok: r.ok, status: r.status, json: async () => r.body ?? {} }
  })
  set('setTimeout', () => { dom.timers++; return 0 })
  set('setInterval', () => 0)
  set('requestAnimationFrame', (fn: () => void) => { dom.frames.push(fn); return 0 })
  set('sessionStorage', {
    getItem: () => null, setItem: () => {}, removeItem: () => {},
  })
  set('alert', () => {})

  return dom
}
