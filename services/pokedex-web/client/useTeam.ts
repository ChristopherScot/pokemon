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
