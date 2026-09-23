import test from 'node:test'
import assert from 'node:assert/strict'

import { board, effectBand, floatsFor, sideFor } from './battle.ts'

type AnyBattle = Parameters<typeof floatsFor>[0]

const mon = (name: string, hp = 10, extra: Record<string, unknown> = {}) => ({
  name, types: ['electric'], hp, maxHp: 10, fainted: hp === 0, sprite: `/${name}.png`,
  moves: [{ name: 'thunderbolt', type: 'electric', power: 90 }, { name: 'tackle', type: 'normal', power: 40 }],
  ...extra,
})

const battle = (over: Partial<AnyBattle> = {}): AnyBattle => ({
  id: 'b1', status: 'active', version: 1, turn: 'ash',
  sides: [
    { trainer: 'ash', team: [mon('pikachu')] },
    { trainer: 'misty', team: [mon('staryu')] },
  ],
  log: [],
  ...over,
} as AnyBattle)

const damage = (target: string, dmg: number, eff?: number, turnNumber = 1) =>
  ({ turnNumber, text: `hit ${target}`, target, damage: dmg, effectiveness: eff })

test('effectBand names the bands the CSS styles', () => {
  assert.equal(effectBand(undefined), 'normal')
  assert.equal(effectBand(null), 'normal')
  assert.equal(effectBand(0), 'immune')
  assert.equal(effectBand(2), 'super')
  assert.equal(effectBand(0.5), 'weak')
  assert.equal(effectBand(1), 'normal')
})

// The float's whole reason for existing server-side is that morph
// preserves a node whose id and attributes have not changed. If the id
// were derived from anything that moves - a counter, a timestamp - the
// animation would restart on every poll, which is the bug this port was
// specifically warned about.
test('a float keeps the same id across polls, so morph leaves it alone', () => {
  const b = battle({ log: [damage('staryu', 4)] })
  const first = floatsFor(b, 0)
  const later = floatsFor(b, 0)
  assert.equal(first.length, 1)
  assert.deepEqual(first.map((f) => f.id), later.map((f) => f.id))
  assert.equal(first[0].id, 'f-0-them-0')
})

// Floats show the LATEST turn, which is what "what just happened"
// means. This used to be decided by a wall clock kept in the server's
// memory - which worked, but meant the service could only run as one
// copy, because a second copy would keep its own separate record.
test('only the latest turn floats', () => {
  const b = battle({
    log: [
      damage('staryu', 4, undefined, 1),
      damage('staryu', 6, undefined, 2),
      damage('pikachu', 3, undefined, 2),
    ],
  })
  const floats = floatsFor(b, 0)
  assert.equal(floats.length, 2, 'both hits from turn 2, neither from turn 1')
  assert.ok(floats.every((f) => !f.text.includes('-4')), 'turn 1 should not float')
})

// The same battle gives the same answer however many times it is
// rendered, on whichever copy of the service. That is the property the
// in-memory version could not offer.
test('rendering the same battle twice gives identical floats', () => {
  const b = battle({ log: [damage('staryu', 4)] })
  assert.deepEqual(floatsFor(b, 0), floatsFor(b, 0))
  // And a second "server" - a separate call with no shared state -
  // agrees, which is what makes more than one replica safe.
  assert.deepEqual(floatsFor(b, 0), floatsFor(structuredClone(b), 0))
})

test('an immune hit is named rather than shown as -0', () => {
  const b = battle({ log: [damage('staryu', 0, 0)] })
  const [f] = floatsFor(b, 0)
  assert.equal(f.band, 'immune')
  assert.equal(f.text, 'no effect')
})

test('a float lands on the side it belongs to', () => {
  const b = battle({ log: [damage('pikachu', 3)] })
  assert.equal(floatsFor(b, 0)[0].slot, 'me-0')
  // The same event seen by the OTHER trainer is on their opponent.
  assert.equal(floatsFor(b, 1)[0].slot, 'them-0')
})

test('sideFor finds both sides, and nothing for an onlooker', () => {
  const b = battle()
  assert.equal(sideFor(b, 'ash')?.mine.trainer, 'ash')
  assert.equal(sideFor(b, 'ash')?.theirs.trainer, 'misty')
  assert.equal(sideFor(b, 'brock'), null)
})

// A busy log used to be a hazard: the history was bounded, so an
// entry could be dropped while its float was still on screen and the
// float would outlive its animation. Freshness now comes from the turn
// number, so the length of the log does not matter at all.
test('a busy log does not float stale turns', () => {
  const log = [damage('staryu', 4, undefined, 1)]
  for (let i = 0; i < 20; i++) log.push(damage('staryu', 1, undefined, 2))
  const b = battle({ log })

  const floats = floatsFor(b, 0)
  assert.equal(floats.length, 20, 'every hit from the latest turn floats')
  assert.ok(!floats.some((f) => f.id === 'f-0-them-0'),
    'the turn-1 hit should not still be floating')
})

// Battles live in the API's memory, so a restart - or a reused id -
// gives this service a shorter log under an id it has seen before.
// This needed careful handling when birth times were remembered; with
// nothing remembered, a new battle is simply a new battle.
test('a battle that starts over still shows floats', () => {
  const fresh = battle({ log: [damage('staryu', 7)] })
  assert.equal(floatsFor(fresh, 0).length, 1, 'the new battle renders no float')
})
