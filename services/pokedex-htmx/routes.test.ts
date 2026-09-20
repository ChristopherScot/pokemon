import { after, test } from 'node:test'
import assert from 'node:assert/strict'

import { app } from './server.ts'
import { readTeam, readTrainer, readTurn } from './routes.ts'

after(() => app.close())

const req = (cookie: string) => ({ headers: { cookie } }) as never

// A cookie is sent on EVERY request to the origin, so a value this
// service cannot parse is not a one-off error: the page stays broken
// until the user clears it by hand.
test('a malformed resume cookie does not break the page', async () => {
  const res = await app.inject({ method: 'GET', url: '/', headers: { cookie: 'battle=%zz' } })
  assert.notEqual(res.statusCode, 500)
})

test('a trainer cookie that is not a trainer is no trainer', () => {
  for (const raw of ['5', '"hi"', '[]', 'null', '{}', '{"name":"ash"}']) {
    assert.equal(readTrainer(req(`trainer=${encodeURIComponent(raw)}`)), null, raw)
  }
  const ok = readTrainer(req(`trainer=${encodeURIComponent('{"name":"ash","token":"t"}')}`))
  assert.deepEqual(ok, { name: 'ash', token: 't' })
})

// Three of the same pokemon is a legal team as far as the API is
// concerned - it looks each name up independently - so it has to be
// refused here, and a crafted querystring is the way in.
test('a team cannot be three of the same pokemon', () => {
  assert.deepEqual(readTeam(['pikachu', 'pikachu', 'pikachu']), ['pikachu'])
  // Dedupe BEFORE the cap, or a,a,b,c silently loses c.
  assert.deepEqual(readTeam(['a', 'a', 'b', 'c']), ['a', 'b', 'c'])
  assert.deepEqual(readTeam(['a', 'b', 'c', 'd']), ['a', 'b', 'c'])
  assert.deepEqual(readTeam(undefined), [])
  assert.deepEqual(readTeam('solo'), ['solo'])
})

// Index 0 is the only index guaranteed to exist, which is why it is the
// fallback: garbage becomes a legal move rather than an API error.
test('a turn index is always one the board could have produced', () => {
  assert.deepEqual(readTurn({}), { attacker: 0, move: 0, target: 0 })
  assert.deepEqual(readTurn({ attacker: '9' }).attacker, 0)
  assert.deepEqual(readTurn({ move: 'abc' }).move, 0)
  assert.deepEqual(readTurn({ attacker: '-1' }).attacker, 0)
  assert.deepEqual(readTurn({ attacker: '2.5' }).attacker, 0)
  assert.deepEqual(readTurn({ attacker: '2', move: '3', target: '1' }),
    { attacker: 2, move: 3, target: 1 })
})
