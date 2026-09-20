// @vitest-environment jsdom
import { act, render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import type { components } from '@christopherscot/pokedex-client'

import { BattleBoard } from './Battle.tsx'

type Battle = components['schemas']['Battle']
type Mon = components['schemas']['BattlePokemon']
type Move = components['schemas']['Move']

// Real values from pokedex.json rather than invented ones: Move requires
// description, effect, pp and damageClass, and a fixture that satisfies
// the type with placeholder text would still be lying about the data the
// components render.
const move = (over: Partial<Move>): Move => ({
  name: 'tackle', type: 'normal', power: 40,
  description:
    'A physical attack in which the user charges and slams into the target with its whole body.',
  effect: 'Inflicts regular damage with no additional effect.',
  accuracy: 100, pp: 35, damageClass: 'physical',
  ...over,
})

const mon = (name: string, over: Record<string, unknown> = {}): Mon => ({
  name, hp: 20, maxHp: 20, sprite: 's.png', types: ['electric'], fainted: false,
  moves: [
    move({}),
    move({
      name: 'growl', power: 0, pp: 40, damageClass: 'status',
      description:
        'The user growls in an endearing way, making opposing Pok\u00e9mon less wary. This lowers their Attack stats.',
      effect: 'Lowers the target\u2019s Attack by one stage.',
    }),
  ],
  ...over,
})

const battle = (over: Partial<Battle> = {}): Battle => ({
  id: 'abc123', status: 'active', version: 1, turn: 'ash', log: [],
  sides: [
    { trainer: 'ash', team: [mon('pikachu')] },
    { trainer: 'misty', team: [mon('staryu')] },
  ],
  ...over,
})

test('the banner is a live region that says whose turn it is', () => {
  render(<BattleBoard battle={battle()} me="ash" />)
  const banner = screen.getByRole('status')
  expect(banner).toHaveTextContent('your turn')
})

test('the banner names the opponent when it is not your turn', () => {
  render(<BattleBoard battle={battle({ turn: 'misty' })} me="ash" />)
  expect(screen.getByRole('status')).toHaveTextContent('waiting on misty')
})

test('a disabled move is a disabled button', () => {
  const b = battle({
    sides: [
      { trainer: 'ash', team: [mon('pikachu', { disabledMove: 1 })] },
      { trainer: 'misty', team: [mon('staryu')] },
    ],
  })
  render(<BattleBoard battle={b} me="ash" />)
  expect(screen.getByRole('button', { name: /growl/ })).toBeDisabled()
  expect(screen.getByRole('button', { name: /tackle/ })).toBeEnabled()
})

// Every mon is a button; a fainted one is DISABLED rather than a
// different element.
//
// React reconciles by element type, so swapping button for div tore the
// subtree down and gave the HP bar a brand new node with no previous
// width - which is why the drain transition never ran. Measured in
// Chromium: it jumped straight to the final width with
// getAnimations().length === 0.
test('a fainted pokemon cannot be chosen, and is still a button', () => {
  const b = battle({
    sides: [
      { trainer: 'ash', team: [mon('pikachu'), mon('geodude', { fainted: true, hp: 0 })] },
      { trainer: 'misty', team: [mon('staryu')] },
    ],
  })
  render(<BattleBoard battle={b} me="ash" />)

  const alive = screen.getByRole('button', { name: /pikachu/ })
  expect(alive).toBeTruthy()
  expect(alive).not.toBeDisabled()

  // queryByRole skips disabled buttons only for some roles, so ask for
  // it including hidden, then assert what actually matters.
  const fainted = screen.getByRole('button', { name: /geodude/, hidden: true })
  expect(fainted).toBeDisabled()
})

test('a spectator gets a board, not a crash', () => {
  render(<BattleBoard battle={battle()} me="brock" />)
  expect(screen.getByRole('status')).toHaveTextContent('watching ash vs misty')
})

const withLog = (log: Battle['log']) => battle({ log })

test('a hit floats its damage over the target', () => {
  const { rerender } = render(<BattleBoard battle={battle()} me="ash" />)
  rerender(
    <BattleBoard
      battle={withLog([{ turnNumber: 1, text: 'hit', target: 'staryu', damage: 7, effectiveness: 1 }])}
      me="ash"
    />,
  )
  expect(screen.getByText('-7')).toBeTruthy()
})

test('an immune hit says "no effect" rather than "-0"', () => {
  const { rerender } = render(<BattleBoard battle={battle()} me="ash" />)
  rerender(
    <BattleBoard
      battle={withLog([{ turnNumber: 1, text: 'immune', target: 'staryu', damage: 0, effectiveness: 0 }])}
      me="ash"
    />,
  )
  expect(screen.getByText('no effect')).toBeTruthy()
  expect(screen.queryByText('-0')).toBeNull()
})

test('a super-effective hit is marked', () => {
  const { rerender } = render(<BattleBoard battle={battle()} me="ash" />)
  rerender(
    <BattleBoard
      battle={withLog([{ turnNumber: 1, text: 'hit', target: 'staryu', damage: 30, effectiveness: 2 }])}
      me="ash"
    />,
  )
  expect(screen.getByText('-30 !!')).toBeTruthy()
})

// A status move carries no damage field at all and has nothing to show.
test('a status move floats nothing', () => {
  const { rerender } = render(<BattleBoard battle={battle()} me="ash" />)
  rerender(
    <BattleBoard
      battle={withLog([{ turnNumber: 1, text: 'pikachu used growl', target: 'staryu' }])}
      me="ash"
    />,
  )
  expect(screen.queryByText(/^-/)).toBeNull()
  expect(screen.queryByText('no effect')).toBeNull()
})

test('a float expires even while the battle keeps polling', async () => {
  vi.useFakeTimers()
  try {
    const hit = { turnNumber: 1, text: 'hit', target: 'staryu', damage: 7, effectiveness: 1 }
    const { rerender } = render(<BattleBoard battle={battle()} me="ash" />)
    rerender(<BattleBoard battle={withLog([hit])} me="ash" />)
    expect(screen.getByText('-7')).toBeTruthy()

    for (let i = 0; i < 4; i++) {
      await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
      rerender(<BattleBoard battle={{ ...battle({ log: [hit] }), version: 2 + i }} me="ash" />)
    }

    expect(screen.queryByText('-7'), 'the float never expired').toBeNull()
  } finally {
    vi.useRealTimers()
  }
})
