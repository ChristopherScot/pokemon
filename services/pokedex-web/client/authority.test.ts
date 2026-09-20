import { expect, test } from 'vitest'

import type { components } from '@christopherscot/pokedex-client'

import { checkTurn } from './shared.ts'

// The same alias shared.ts uses: the generated client is the source of
// this shape, so a test that declared its own could drift from it.
type Battle = components['schemas']['Battle']

// The server owns the turn rules; a client reads its verdict.
//
// canAct, canBeTargeted and usableMoves ARE the verdict. fainted and
// disabledMove are the inputs the server used to reach it. Deciding
// from the inputs here is a second implementation of the rule, and the
// two can disagree - the server rejects the turn regardless, so the
// only thing a drifted copy changes is whether the UI lies first.
//
// This file exists because that drift already happened. The migration
// that moved the other clients onto the verdict (020b5b7, "the server
// owns the rules, and the clients read them") said "all four clients
// updated" - there are five, and this one was not among them. Nothing
// failed, because nothing asserted it for TypeScript.

const mon = (name: string, over: Record<string, unknown> = {}) => ({
  name,
  hp: 100,
  maxHp: 100,
  fainted: false,
  types: ['normal'],
  moves: [{ name: 'tackle', power: 40 }, { name: 'growl', power: 0 }],
  ...over,
})

const battle = (mine: unknown[], theirs: unknown[]): Battle =>
  ({
    status: 'active',
    turn: 'me',
    sides: [
      { trainer: 'me', team: mine },
      { trainer: 'you', team: theirs },
    ],
  }) as unknown as Battle

const turn = { attacker: 0, move: 0, target: 0 }

test('a pokemon the server says cannot act is refused, whatever fainted says', () => {
  // The disagreement made explicit: not fainted, but the server says
  // it may not act. Reading `fainted` would wave this through and the
  // server would then reject the turn.
  const b = battle([mon('pikachu', { fainted: false, canAct: false })], [mon('onix')])
  expect(checkTurn(b, 'me', turn)).not.toBe('')
})

test('a target the server says cannot be targeted is refused', () => {
  const b = battle(
    [mon('pikachu')],
    [mon('onix', { fainted: false, canBeTargeted: false })],
  )
  expect(checkTurn(b, 'me', turn)).not.toBe('')
})

test('a move the server does not list as usable is refused', () => {
  // disabledMove says move 1; usableMoves says move 0 is the disabled
  // one. The server's answer must win.
  const b = battle(
    [mon('pikachu', { usableMoves: [false, true], disabledMove: 1 })],
    [mon('onix')],
  )
  expect(checkTurn(b, 'me', turn)).toContain('disabled')
})

test('the server saying yes is enough, even when the old inputs would say no', () => {
  const b = battle(
    [mon('pikachu', { canAct: true, usableMoves: [true, true] })],
    [mon('onix', { canBeTargeted: true })],
  )
  expect(checkTurn(b, 'me', turn)).toBe('')
})

// A battle from a server that predates these fields must still work,
// or upgrading the client breaks against an old server.
test('without the verdict fields it falls back to the inputs', () => {
  const fainted = battle([mon('pikachu', { fainted: true })], [mon('onix')])
  expect(checkTurn(fainted, 'me', turn)).toContain('fainted')

  const disabled = battle([mon('pikachu', { disabledMove: 0 })], [mon('onix')])
  expect(checkTurn(disabled, 'me', turn)).toContain('disabled')

  const fine = battle([mon('pikachu')], [mon('onix')])
  expect(checkTurn(fine, 'me', turn)).toBe('')
})
