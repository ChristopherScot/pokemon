// @vitest-environment jsdom
//
// Component tests for the battle screen.
//
// These replace assertions that used to match the SERVER's markup with
// a regex. That worked while the page was a template literal and stops
// being meaningful the moment the board is a component: what matters is
// what React renders, not what the shell HTML contains.
import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { BattleBoard } from './Battle.tsx'

const mon = (name: string, over: Record<string, unknown> = {}) => ({
  name, hp: 20, maxHp: 20, sprite: 's.png', types: ['electric'], fainted: false,
  moves: [
    { name: 'tackle', type: 'normal', power: 40 },
    { name: 'growl', type: 'normal', power: 0 },
  ],
  ...over,
})

const battle = (over: Record<string, unknown> = {}) => ({
  id: 'abc123', status: 'active', version: 1, turn: 'ash', log: [],
  sides: [
    { trainer: 'ash', team: [mon('pikachu')] },
    { trainer: 'misty', team: [mon('staryu')] },
  ],
  ...over,
}) as never

// A screen-reader user has to be TOLD their turn began; it is the one
// thing this game must announce. The old banner was rebuilt inside a
// full innerHTML swap, and a live region that is destroyed and
// recreated announces nothing.
test('the banner is a live region that says whose turn it is', () => {
  render(<BattleBoard battle={battle()} me="ash" />)
  const banner = screen.getByRole('status')
  expect(banner).toHaveTextContent('your turn')
})

test('the banner names the opponent when it is not your turn', () => {
  render(<BattleBoard battle={battle({ turn: 'misty' })} me="ash" />)
  expect(screen.getByRole('status')).toHaveTextContent('waiting on misty')
})

// The spec says of disabledMove: "Selecting it is a 409, so a client
// should show it as unavailable rather than letting the turn fail."
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

// Every Pokemon you can choose is a real button, so the board is
// reachable by keyboard. It used to be a div with a delegated click
// handler, which made the game mouse-only.
test('choosable pokemon are buttons, and fainted ones are not', () => {
  const b = battle({
    sides: [
      { trainer: 'ash', team: [mon('pikachu'), mon('geodude', { fainted: true, hp: 0 })] },
      { trainer: 'misty', team: [mon('staryu')] },
    ],
  })
  render(<BattleBoard battle={b} me="ash" />)
  expect(screen.getByRole('button', { name: /pikachu/ })).toBeTruthy()
  expect(screen.queryByRole('button', { name: /geodude/ })).toBeNull()
})

// A non-participant sees the battle rather than a broken board. The
// arithmetic that produced this bug - 1 - findIndex(...) with -1 - made
// BOTH sides undefined and left the page on "loading…" forever.
test('a spectator gets a board, not a crash', () => {
  render(<BattleBoard battle={battle()} me="brock" />)
  expect(screen.getByRole('status')).toHaveTextContent('watching ash vs misty')
})

// The damage float, restored after the React port dropped it.
//
// The bug it exists to prevent: the old loop was guarded by
// `!e.damage`, and an immune hit deals exactly 0, so the branch that
// says "no effect" was unreachable - the classifier was right and the
// screen showed nothing.
const withLog = (log: unknown[]) => battle({ log }) as never

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
