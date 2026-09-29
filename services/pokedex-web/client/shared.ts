
import type { components } from '@christopherscot/pokedex-client'

import { UI_VERSION } from '../version.ts'

type Battle = components['schemas']['Battle']
export type Turn = { attacker: number; move: number; target: number }

export const TEAM_SIZE = 3

export { UI_VERSION }

export const V = { 'Client-Version': UI_VERSION }

export const TERMINAL = new Set([410, 501, 505])

const ENTITIES: Record<string, string> = {
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
}
export const esc = (t: unknown) =>
  String(t).replace(/[&<>"']/g, (c) => ENTITIES[c] ?? c)

export const checkTurn = (b: Battle, me: string, t: Turn): string => {
  if (b.status === 'finished') return 'this battle is over'
  if (b.status !== 'active') return 'this battle has not started'

  const mineIdx = b.sides.findIndex((s) => s.trainer === me)
  if (mineIdx === -1 || b.turn !== me) return 'not your turn'
  const mine = b.sides[mineIdx], theirs = b.sides[1 - mineIdx]

  if (!mine.team[t.attacker]) return 'you have no pokemon ' + (t.attacker + 1)
  if (!theirs.team[t.target]) return 'they have no pokemon ' + (t.target + 1)

  // Prefer server-computed fields (canAct/canBeTargeted/usableMoves); fall back to raw inputs (fainted/disabledMove) for older servers.
  const attacker = mine.team[t.attacker]
  const target = theirs.team[t.target]

  if (!(attacker.canAct ?? !attacker.fainted)) return attacker.name + ' has fainted'
  if (!attacker.moves[t.move]) return attacker.name + ' has no move ' + (t.move + 1)
  if (!(target.canBeTargeted ?? !target.fainted)) return target.name + ' has already fainted'

  const usable = attacker.usableMoves
    ? attacker.usableMoves[t.move]
    : attacker.disabledMove !== t.move
  if (!usable) return attacker.moves[t.move].name + ' is disabled this turn'
  return ''
}

export const effectBand = (e: number | null | undefined) => {
  if (e === undefined || e === null) return 'normal'
  if (e === 0) return 'immune'
  if (e >= 2) return 'super'
  if (e < 1) return 'weak'
  return 'normal'
}
