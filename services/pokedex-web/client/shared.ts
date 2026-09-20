// What every page's browser code needs.
//
// This was PRELUDE: a template literal pasted into two <script> tags,
// because two inline scripts are two module scopes and a helper defined
// in one was simply absent in the other. That cost a production outage
// - every lobby handler threw "ReferenceError: V is not defined" on its
// first statement, silently, because module scripts fail quietly.
//
// It is a module now, so the compiler sees it and an import either
// resolves or fails the build.

import type { components } from '@christopherscot/pokedex-client'

import { UI_VERSION } from '../version.ts'

type Battle = components['schemas']['Battle']
export type Turn = { attacker: number; move: number; target: number }

export { UI_VERSION }

// Sent on every request this page makes, so the server knows which
// browser code is calling it.
export const V = { 'Client-Version': UI_VERSION }

// Statuses that mean "stop asking" rather than "try again". 410 is the
// server refusing this client as too old; retrying cannot help, because
// the code in this tab will not change on its own.
export const TERMINAL = new Set([410, 501, 505])

const ENTITIES: Record<string, string> = {
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
}
export const esc = (t: unknown) =>
  String(t).replace(/[&<>"']/g, (c) => ENTITIES[c] ?? c)

// The one place the web decides what a multiplier MEANS. The server
// sends a raw number; every display that wants a word or a colour asks
// here rather than re-deriving thresholds.
//
// Zero is immune, not weak. That distinction was wrong in one of the
// two places this replaces: the battle log excluded 0 with an explicit
// > 0 guard, the floating damage number did not, so the same immune hit
// was styled two different ways on the same screen.
// Why a turn cannot be played, or "" if it can.
//
// The order mirrors the server's takeTurn and the Go clients'
// battleclient.CheckTurn, deliberately: a client that reports a
// different FIRST reason than the authority teaches a rule that is not
// the rule. "that pokemon has fainted" where the server would say "not
// your turn" is worse than saying nothing, because it is confidently
// wrong.
//
// This does not replace the server's check. The board is up to a poll
// behind and the opponent moves too, so a locally-legal turn can still
// be rejected - attack() still shows that. What this removes is the
// turn that was knowably illegal before it was sent.
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
  // The spec says of disabledMove: "Selecting it is a 409, so a client
  // should show it as unavailable rather than letting the turn fail."
  // This page was not reading the field at all.
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
