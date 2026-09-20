// @vitest-environment jsdom
//
// The poll loop's rules, which all came from real incidents.
import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { useBattle } from './useBattle.ts'

afterEach(() => { vi.unstubAllGlobals() })

function replyWith(replies: Array<{ ok: boolean; status: number; body?: unknown }>) {
  let i = 0
  const calls: string[] = []
  vi.stubGlobal('fetch', async (url: string) => {
    calls.push(url)
    const r = replies[Math.min(i++, replies.length - 1)]
    return { ok: r.ok, status: r.status, json: async () => r.body ?? {} }
  })
  return calls
}

// A 404 means the battle is GONE. Retrying it forever is what produced
// 46,000 requests from one tab over thirteen hours, and no deploy could
// reach that tab to stop it.
test('polling stops once the battle is gone, and says why', async () => {
  replyWith([{ ok: false, status: 404 }])
  const { result } = renderHook(() => useBattle('abc123'))

  await waitFor(() => expect(result.current.kind).toBe('stopped'), { timeout: 15_000 })
  if (result.current.kind !== 'stopped') throw new Error('unreachable')
  expect(result.current.message).toMatch(/over/)
}, 20_000)

// 410 means the server is refusing THIS client as too old. Retrying
// cannot help: the code in this tab will not change on its own.
test('a terminal status stops immediately rather than retrying', async () => {
  const calls = replyWith([{ ok: false, status: 410 }])
  const { result } = renderHook(() => useBattle('abc123'))

  await waitFor(() => expect(result.current.kind).toBe('stopped'))
  expect(calls.length).toBe(1)
  if (result.current.kind !== 'stopped') throw new Error('unreachable')
  expect(result.current.message).toMatch(/out of date/)
})

// A good response becomes a battle, and the version is what decides
// whether anything changed.
test('a battle arrives and is reported once per version', async () => {
  replyWith([{ ok: true, status: 200, body: { id: 'abc123', version: 7, sides: [], log: [] } }])
  const { result } = renderHook(() => useBattle('abc123'))

  await waitFor(() => expect(result.current.kind).toBe('ok'))
  if (result.current.kind !== 'ok') throw new Error('unreachable')
  expect(result.current.battle.version).toBe(7)
})

// Every request says which browser code is calling, so the server can
// see - and refuse - a client that is doing harm.
test('every request reports the UI version', async () => {
  const seen: Array<Record<string, string>> = []
  vi.stubGlobal('fetch', async (_url: string, init?: { headers?: Record<string, string> }) => {
    seen.push(init?.headers ?? {})
    return { ok: true, status: 200, json: async () => ({ id: 'a', version: 1, sides: [], log: [] }) }
  })
  const { result } = renderHook(() => useBattle('abc123'))

  await waitFor(() => expect(result.current.kind).toBe('ok'))
  expect(seen[0]['Client-Version']).toBeTruthy()
})

// A new battle gets a fresh miss budget.
//
// seen and misses were refs on the component, not the polling run, so
// they outlived a change of id: after one battle spent its five misses,
// the next one stopped on its FIRST 404 and told the player "this
// battle is over" about a battle that was merely slow. Unreachable
// while the page mounts per battle, which is what made it a trap for
// whoever adds client routing rather than a visible bug.
test('switching battles resets the miss budget', async () => {
  const calls: string[] = []
  vi.stubGlobal('fetch', async (url: string) => {
    calls.push(url)
    return { ok: false, status: 404, json: async () => ({}) }
  })

  const { result, rerender } = renderHook(({ id }) => useBattle(id), {
    initialProps: { id: 'first' },
  })
  await waitFor(() => expect(result.current.kind).toBe('stopped'), { timeout: 15_000 })
  const spent = calls.filter((u) => u.includes('first')).length
  expect(spent).toBeGreaterThan(1)

  // The hook still reports 'stopped' from the FIRST battle until the
  // second decides otherwise, so waiting on state returns instantly.
  // Count the requests the second battle actually makes instead.
  calls.length = 0
  rerender({ id: 'second' })
  await waitFor(
    () => expect(calls.filter((u) => u.includes('second')).length).toBe(spent),
    { timeout: 15_000 },
  )
}, 40_000)
