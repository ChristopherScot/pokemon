import { useEffect, useState } from 'react'

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

  useEffect(() => {
    let live = true
    let timer: ReturnType<typeof setTimeout> | undefined

    let seen = -1
    let misses = 0

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
          misses += 1
          if (misses >= MAX_MISSES) {
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
          misses = 0
          const b = (await res.json()) as Battle
          if (b.version !== seen) {
            seen = b.version
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
