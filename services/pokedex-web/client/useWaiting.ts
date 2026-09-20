// The lobby's waiting list, refreshed on a timer.
//
// `initial` is what the server already rendered into the page, so the
// list is correct on first paint and this only replaces it once the
// first poll comes back. That is why it seeds state rather than being
// read on every render: after mount, the server's copy is the stale
// one.
//
// Paused while the tab is hidden. A lobby left open in a background tab
// is the shape that produced 46,000 wasted requests from one page.
import { useEffect, useState } from 'react'

import type { components } from '@christopherscot/pokedex-client'

import { V } from './shared.ts'

type Waiting = components['schemas']['WaitingBattle']

const POLL_MS = 3000

export function useWaiting(initial: Waiting[]): Waiting[] {
  const [waiting, setWaiting] = useState(initial)

  useEffect(() => {
    let live = true
    let timer: ReturnType<typeof setTimeout> | undefined

    async function tick() {
      if (!live) return
      if (!document.hidden) {
        try {
          const res = await fetch('/battle/waiting', { headers: V })
          if (res.ok && live) {
            const body = (await res.json()) as { waiting?: Waiting[] }
            setWaiting(body.waiting ?? [])
          }
        } catch {
          // A dropped poll is not worth showing; the next one retries.
        }
      }
      if (live) timer = setTimeout(tick, POLL_MS)
    }

    // Starts after one interval, not immediately: the server already
    // gave us this list, so an instant refetch would ask for what we
    // are currently displaying.
    timer = setTimeout(tick, POLL_MS)
    return () => {
      live = false
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [])

  return waiting
}
