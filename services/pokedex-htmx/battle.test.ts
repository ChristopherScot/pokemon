import test from 'node:test'
import assert from 'node:assert/strict'

import { board, effectBand, floatsFor, LIFE_MS, observe, sideFor } from './battle.ts'

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

const damage = (target: string, dmg: number, eff?: number) =>
  ({ turnNumber: 1, text: `hit ${target}`, target, damage: dmg, effectiveness: eff })

test('effectBand names the bands the CSS styles', () => {
  assert.equal(effectBand(undefined), 'normal')
  assert.equal(effectBand(null), 'normal')
  assert.equal(effectBand(0), 'immune')
  assert.equal(effectBand(2), 'super')
  assert.equal(effectBand(0.5), 'weak')
  assert.equal(effectBand(1), 'normal')
})

test('a float keeps the same id across polls, so morph leaves it alone', () => {
  const b = battle({ log: [damage('staryu', 4)] })
  observe(b, 1000)
  const first = floatsFor(b, 0, 1000)
  const later = floatsFor(b, 0, 1900)
  assert.equal(first.length, 1)
  assert.deepEqual(first.map((f) => f.id), later.map((f) => f.id))
  assert.equal(first[0].id, 'f-0-them-0')
})

test('a float expires exactly at its CSS lifetime', () => {
  const b = battle({ log: [damage('staryu', 4)] })
  observe(b, 1000)
  assert.equal(floatsFor(b, 0, 1000 + LIFE_MS - 1).length, 1)
  assert.equal(floatsFor(b, 0, 1000 + LIFE_MS).length, 0)
})

test('polling repeatedly does not restart a float\'s clock', () => {
  const b = battle({ log: [damage('staryu', 4)] })
  observe(b, 1000)
  for (let t = 1050; t < 3000; t += 50) observe(b, t)
  assert.equal(floatsFor(b, 0, 1000 + LIFE_MS - 1).length, 1, 'alive just before its lifetime')
  assert.equal(floatsFor(b, 0, 1000 + LIFE_MS).length, 0, 'expired on schedule, not restarted')
})

test('an immune hit is named rather than shown as -0', () => {
  const b = battle({ log: [damage('staryu', 0, 0)] })
  observe(b, 1000)
  const [f] = floatsFor(b, 0, 1000)
  assert.equal(f.band, 'immune')
  assert.equal(f.text, 'no effect')
})

test('a float lands on the side it belongs to', () => {
  const b = battle({ log: [damage('pikachu', 3)] })
  observe(b, 1000)
  assert.equal(floatsFor(b, 0, 1000)[0].slot, 'me-0')
  assert.equal(floatsFor(b, 1, 1000)[0].slot, 'them-0')
})

test('sideFor finds both sides, and nothing for an onlooker', () => {
  const b = battle()
  assert.equal(sideFor(b, 'ash')?.mine.trainer, 'ash')
  assert.equal(sideFor(b, 'ash')?.theirs.trainer, 'misty')
  assert.equal(sideFor(b, 'brock'), null)
})

test('a float still expires when the log is busy', () => {
  const b = battle({ log: [damage('staryu', 4)] })
  observe(b, 1000)
  for (let i = 1; i <= 20; i++) {
    b.log.push(damage('staryu', 1))
    observe(b, 1000 + i * 80)
  }
  const alive = floatsFor(b, 0, 1000 + LIFE_MS + 10).some((f) => f.id === 'f-0-them-0')
  assert.equal(alive, false, 'the first float outlived its animation')
})

test('a battle that starts over still shows floats', () => {
  const b = battle({ log: [damage('staryu', 4), damage('staryu', 4), damage('staryu', 4)] })
  observe(b, 1000)
  const fresh = battle({ log: [damage('staryu', 7)] })
  observe(fresh, 9000)
  assert.equal(floatsFor(fresh, 0, 9000).length, 1, 'the new battle renders no float')
})
