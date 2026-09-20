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

    timer = setTimeout(tick, POLL_MS)
    return () => {
      live = false
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [])

  return waiting
}
