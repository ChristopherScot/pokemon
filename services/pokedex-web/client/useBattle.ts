// Polling a battle, as a hook.
//
// The rules it encodes are the ones that came out of real incidents,
// and they are why this is not just setInterval(fetch):
//
//   - A 404 means the battle is GONE, and retrying it forever is what
//     produced 46,000 requests from one tab over thirteen hours. Five
//     consecutive misses and it stops, saying why.
//   - 410/501/505 mean the server is refusing THIS client. Retrying
//     cannot help, because the code in this tab will not change on its
//     own, so those stop immediately.
//   - Anything else is transient and worth another go.
//   - A hidden tab does not poll. A lobby left open in a background tab
//     is the shape that produced the 46,000 requests.
import { useEffect, useRef, useState } from 'react'

import type { components } from '@christopherscot/pokedex-client'

import { V, TERMINAL } from './shared.ts'

type Battle = components['schemas']['Battle']

export type BattleState =
  | { kind: 'loading' }
  | { kind: 'ok'; battle: Battle }
  | { kind: 'stopped'; message: string }

const MAX_MISSES = 5
const INTERVAL = 1000

export function useBattle(id: string): BattleState {
  const [state, setState] = useState<BattleState>({ kind: 'loading' })
  // Refs, not state: changing these must not re-render, and the poll
  // loop has to see the current value rather than the one captured
  // when the effect ran.
  const seen = useRef(-1)
  const misses = useRef(0)

  useEffect(() => {
    let live = true
    let timer: ReturnType<typeof setTimeout> | undefined

    async function tick() {
      if (!live) return
      if (typeof document !== 'undefined' && document.hidden) {
        timer = setTimeout(tick, INTERVAL)
        return
      }
      try {
        const res = await fetch(`/battle/${id}/state`, { headers: V })
        if (!live) return

        if (res.status === 404) {
          misses.current += 1
          if (misses.current >= MAX_MISSES) {
            setState({
              kind: 'stopped',
              message: 'this battle is over — the server restarted, or it expired',
            })
            return
          }
        } else if (TERMINAL.has(res.status)) {
          setState({
            kind: 'stopped',
            message: 'this page is out of date — reload to pick up the new version',
          })
          return
        } else if (res.ok) {
          misses.current = 0
          const b = (await res.json()) as Battle
          // Only re-render when the server says something changed;
          // repainting an identical board fights the animations.
          if (b.version !== seen.current) {
            seen.current = b.version
            setState({ kind: 'ok', battle: b })
          }
        }
      } catch {
        // A dropped poll is not worth showing; the next one retries.
      }
      if (live) timer = setTimeout(tick, INTERVAL)
    }

    void tick()
    return () => {
      live = false
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [id])

  return state
}
