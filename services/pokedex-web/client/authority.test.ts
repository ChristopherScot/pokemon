// Guards against the client re-deriving turn rules from the raw inputs instead of the server's verdict fields.
import { expect, test } from 'vitest'

import type { components } from '@christopherscot/pokedex-client'

import { checkTurn } from './shared.ts'

type Battle = components['schemas']['Battle']

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

// Backward compat: a battle from a server that predates these fields must still work.
test('without the verdict fields it falls back to the inputs', () => {
  const fainted = battle([mon('pikachu', { fainted: true })], [mon('onix')])
  expect(checkTurn(fainted, 'me', turn)).toContain('fainted')

  const disabled = battle([mon('pikachu', { disabledMove: 0 })], [mon('onix')])
  expect(checkTurn(disabled, 'me', turn)).toContain('disabled')

  const fine = battle([mon('pikachu')], [mon('onix')])
  expect(checkTurn(fine, 'me', turn)).toBe('')
})
