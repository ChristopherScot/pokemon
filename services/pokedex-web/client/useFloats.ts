import { useEffect, useRef, useState } from 'react'

import type { components } from '@christopherscot/pokedex-client'

import { effectBand } from './shared.ts'

type Battle = components['schemas']['Battle']

export type Float = {
  id: number
  slot: string
  text: string
  band: string
}

const LIFE_MS = 2400

let nextId = 0

export function useFloats(battle: Battle | null, mineIdx: number): Float[] {
  const [floats, setFloats] = useState<Float[]>([])
  const seenLog = useRef(-1)
  const pending = useRef(new Set<ReturnType<typeof setTimeout>>())

  useEffect(() => {
    if (!battle) return
    const log = battle.log
    if (seenLog.current < 0) { seenLog.current = log.length; return }
    if (log.length <= seenLog.current) return

    const fresh: Float[] = []
    for (const e of log.slice(seenLog.current)) {
      if (e.damage === undefined || e.damage === null || !e.target) continue
      for (const [si, side] of battle.sides.entries()) {
        const i = side.team.findIndex((p) => p.name === e.target)
        if (i < 0) continue
        const band = effectBand(e.effectiveness)
        fresh.push({
          id: nextId++,
          slot: `${si === mineIdx ? 'me' : 'them'}-${i}`,
          // An immune hit deals 0, so "-0" says nothing. Name it.
          text: band === 'immune' ? 'no effect' : `-${e.damage}${band === 'super' ? ' !!' : ''}`,
          band,
        })
      }
    }
    seenLog.current = log.length
    if (fresh.length === 0) return

    setFloats((cur) => [...cur, ...fresh])

    const ids = new Set(fresh.map((f) => f.id))
    const timer = setTimeout(
      () => setFloats((cur) => cur.filter((f) => !ids.has(f.id))),
      LIFE_MS,
    )
    pending.current.add(timer)
  }, [battle, mineIdx])

  useEffect(() => {
    const timers = pending.current
    return () => { for (const t of timers) clearTimeout(t) }
  }, [])

  return floats
}
