// The team you are building, held in sessionStorage.
//
// It survives a filter change and a reload because choosing three
// Pokemon out of a hundred means scrolling and filtering, and losing
// the selection to a click on "fire" would be infuriating.
import { useCallback, useEffect, useState } from 'react'

import { TEAM_SIZE } from './shared.ts'

export type Pick = { name: string; sprite: string }

const KEY = 'pokedex.team'

function read(): Pick[] {
  try {
    const raw = sessionStorage.getItem(KEY)
    return raw ? (JSON.parse(raw) as Pick[]).slice(0, TEAM_SIZE) : []
  } catch {
    // Private mode, or something else wrote nonsense here.
    return []
  }
}

export function useTeam() {
  // A lazy initialiser, not an effect. `read` is PASSED, not called:
  // React runs it once, before the first render.
  //
  // This was an effect, on the reasoning that sessionStorage does not
  // exist while the server renders. It does not apply - these
  // components are never server-rendered; the server ships an empty
  // <div id="root"> and React only ever mounts in the browser, so
  // there is no hydration and no mismatch to avoid. And the effect
  // version is actively wrong: StrictMode double-invokes effects, so
  // the write effect ran with the initial [] and erased the saved team
  // before the read effect could restore it. A player picked three
  // Pokemon, reloaded, and they were gone AND overwritten.
  //
  // For a real server-rendered app the answer is useSyncExternalStore
  // or a post-hydration effect - not this. The rule is "client-only
  // mount -> lazy initialiser", not "never read storage in useState".
  const [team, setTeam] = useState<Pick[]>(read)

  useEffect(() => {
    try { sessionStorage.setItem(KEY, JSON.stringify(team)) } catch { /* private mode */ }
  }, [team])

  const toggle = useCallback((pick: Pick) => {
    setTeam((current) => {
      const at = current.findIndex((t) => t.name === pick.name)
      if (at >= 0) return current.filter((_, i) => i !== at)
      if (current.length >= TEAM_SIZE) return current
      return [...current, pick]
    })
  }, [])

  const removeAt = useCallback((i: number) => {
    setTeam((current) => current.filter((_, j) => j !== i))
  }, [])

  const clear = useCallback(() => {
    setTeam([])
    try { sessionStorage.removeItem(KEY) } catch { /* private mode */ }
  }, [])

  return { team, toggle, removeAt, clear }
}
