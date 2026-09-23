import test from 'node:test'
import assert from 'node:assert/strict'

import { board } from './battle.ts'

type BoardArg = Parameters<typeof board>[0]
type AnyBattle = BoardArg['b']

// One cast, here, rather than at every call: these fixtures are shaped
// by hand to match what the server sends, which is the whole point of
// the file.
const render = (b: unknown, target: number): string =>
  board({ b, me: 'ash', sel: { attacker: 0, move: 0, target } } as unknown as BoardArg)

// serverMon is a pokemon shaped the way the SERVER publishes one.
//
// The shared fixture in battle.test.ts sets neither canAct nor
// canBeTargeted, so every existing test exercises the `?? !fainted`
// compatibility fallback - a path a live server never produces. That
// is why targeting could be completely broken while the suite stayed
// green.
//
// The API publishes them asymmetrically (services/pokedex/battle.go):
//
//   CanAct:        sideActive && c.canAct()
//   CanBeTargeted: !sideActive && c.canBeTargeted()
//
// so on your turn EVERY opponent has canAct false.
const serverMon = (name: string, sideActive: boolean, fainted = false): Record<string, unknown> => ({
  name, types: ['water'], hp: fainted ? 0 : 10, maxHp: 10, fainted,
  sprite: `/${name}.png`,
  moves: [{ name: 'tackle', type: 'normal', power: 40 }],
  canAct: sideActive && !fainted,
  canBeTargeted: !sideActive && !fainted,
  usableMoves: [true],
})

const liveBattle = (over: Record<string, unknown> = {}): AnyBattle => ({
  id: 'b1', status: 'active', version: 1, turn: 'ash',
  sides: [
    { trainer: 'ash', team: [serverMon('pikachu', true), serverMon('charmander', true)] },
    { trainer: 'misty', team: [serverMon('staryu', false), serverMon('psyduck', false)] },
  ],
  log: [],
  ...over,
} as unknown as AnyBattle)

// You can choose which of their pokemon to attack.
//
// sideBlock used one predicate for both sides. Reading canAct for the
// OPPONENT is always false, so no name="target" radio was rendered at
// all: the form posted no target, readTurn fell back to 0, and every
// attack hit their first slot - while the button said "attack staryu".
// In a 3-v-3 game that is most of the tactics, gone silently.
test('a live opponent can be chosen as the target', () => {
  const html = render(liveBattle(), 0)

  assert.ok(html.includes('name="target"'),
    'no target radio was rendered; the turn form cannot say which pokemon to attack')

  const targets = (html.match(/name="target"/g) ?? []).length
  assert.equal(targets, 2,
    `rendered ${targets} target radios, want 2 - both live opponents must be choosable`)
})

// And your own side still selects on canAct.
test('your own pokemon are chosen as the attacker', () => {
  const html = render(liveBattle(), 0)
  const attackers = (html.match(/name="attacker"/g) ?? []).length
  assert.equal(attackers, 2, `rendered ${attackers} attacker radios, want 2`)
})

// A fainted opponent is not a target, so the verdict is doing real
// work rather than just being permissive.
test('a fainted opponent cannot be targeted', () => {
  const b = liveBattle({
    sides: [
      { trainer: 'ash', team: [serverMon('pikachu', true)] },
      { trainer: 'misty', team: [serverMon('staryu', false, true), serverMon('psyduck', false)] },
    ],
  })
  const html = render(b, 1)
  const targets = (html.match(/name="target"/g) ?? []).length
  assert.equal(targets, 1, `rendered ${targets} target radios, want 1 - staryu has fainted`)
})
