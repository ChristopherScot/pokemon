
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

  const attacker = mine.team[t.attacker]
  if (attacker.fainted) return attacker.name + ' has fainted'
  if (!attacker.moves[t.move]) return attacker.name + ' has no move ' + (t.move + 1)
  if (theirs.team[t.target].fainted) return theirs.team[t.target].name + ' has already fainted'
  if (attacker.disabledMove === t.move) return attacker.moves[t.move].name + ' is disabled this turn'
  return ''
}

export const effectBand = (e: number | null | undefined) => {
  if (e === undefined || e === null) return 'normal'
  if (e === 0) return 'immune'
  if (e >= 2) return 'super'
  if (e < 1) return 'weak'
  return 'normal'
}
