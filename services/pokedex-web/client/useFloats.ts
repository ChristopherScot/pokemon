// The damage numbers that rise off a Pokemon when it is hit.
//
// Derived from the battle LOG rather than from HP changing, because the
// log is what says who hit whom and how effective it was. Only entries
// that arrived since the last render animate: a battle joined mid-way
// would otherwise replay every hit that already happened and then go
// quiet for the turns you are actually there for.
import { useEffect, useRef, useState } from 'react'

import type { components } from '@christopherscot/pokedex-client'

import { effectBand } from './shared.ts'

type Battle = components['schemas']['Battle']

export type Float = {
  id: number
  /** "me-0" / "them-2" - which slot to float over. */
  slot: string
  text: string
  band: string
}

/** How long a float stays on screen, matching the CSS animation. */
const LIFE_MS = 2400

let nextId = 0

export function useFloats(battle: Battle | null, mineIdx: number): Float[] {
  const [floats, setFloats] = useState<Float[]>([])
  // -1 means "not rendered yet": the first pass adopts the log's length
  // rather than replaying it.
  const seenLog = useRef(-1)
  const pending = useRef(new Set<ReturnType<typeof setTimeout>>())

  useEffect(() => {
    if (!battle) return
    const log = battle.log
    if (seenLog.current < 0) { seenLog.current = log.length; return }
    if (log.length <= seenLog.current) return

    const fresh: Float[] = []
    for (const e of log.slice(seenLog.current)) {
      // A plain truthiness test would skip a 0-damage hit, which is
      // exactly the immune case that most deserves a float saying so.
      // Only an event with no damage field at all - a status move or
      // pure narration - has nothing to show.
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

    // Scheduled here, and NOT returned as cleanup.
    //
    // It was cleanup, which cancelled it on the next poll - and
    // useBattle hands down a new battle object every second, so the
    // effect re-ran and killed the timer before it ever fired. The
    // floats never expired and the shake never stopped: one hit and
    // the Pokemon jittered forever.
    //
    // A float's lifetime belongs to the float, not to the effect that
    // happened to create it.
    const ids = new Set(fresh.map((f) => f.id))
    const timer = setTimeout(
      () => setFloats((cur) => cur.filter((f) => !ids.has(f.id))),
      LIFE_MS,
    )
    pending.current.add(timer)
  }, [battle, mineIdx])

  // Only unmount cancels them, so a screen that goes away does not
  // leave timers running.
  useEffect(() => {
    const timers = pending.current
    return () => { for (const t of timers) clearTimeout(t) }
  }, [])

  return floats
}
