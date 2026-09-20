// @vitest-environment jsdom
import { StrictMode } from 'react'

import { render, screen, waitFor } from '@testing-library/react'
import { userEvent } from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { Pokedex } from './Pokedex.tsx'

const mon = (id: number, name: string) => ({
  id, name, sprite: `${name}.png`, types: ['electric'],
  height: 4, weight: 60,
  moves: [{ name: 'thunderbolt', type: 'electric', power: 90 }],
}) as never

const dex = [mon(25, 'pikachu'), mon(1, 'bulbasaur'), mon(4, 'charmander')]

beforeEach(() => { sessionStorage.clear() })
afterEach(() => { vi.unstubAllGlobals() })

// The card grid IS the team picker. As an <article> with a delegated
// click handler the whole product was mouse-only: you could browse and
// filter and then not pick a team.
test('every card is a real button, with its selected state announced', async () => {
  const user = userEvent.setup()
  render(<Pokedex pokemon={dex} types={[]} active="" join="" resume="" />)

  const card = screen.getByRole('button', { name: /pikachu/ })
  expect(card).toHaveAttribute('aria-pressed', 'false')
  await user.click(card)
  expect(card).toHaveAttribute('aria-pressed', 'true')
})

// Picking a team must work the same way whether you are opening a
// battle or joining one. The grid used to be wired only to "Ready to
// battle", so everyone joining got a bare text box and had to type
// three names from memory.
test('with ?join the button joins THAT battle, carrying the team', async () => {
  const user = userEvent.setup()
  const loc = { href: '', reload: vi.fn() }
  vi.stubGlobal('location', loc)
  const calls: Array<{ url: string; body: string }> = []
  vi.stubGlobal('fetch', async (url: string, init?: { body?: string }) => {
    calls.push({ url, body: init?.body ?? '' })
    return { ok: true, status: 200, json: async () => ({ id: 'ignored' }) }
  })

  render(<Pokedex pokemon={dex} types={[]} active="" join="abc123" resume="" />)
  expect(screen.getByRole('button', { name: 'Join battle' })).toBeTruthy()

  await user.click(screen.getByRole('button', { name: /pikachu/ }))
  await user.click(screen.getByRole('button', { name: 'Join battle' }))

  await waitFor(() => expect(calls.length).toBe(1))
  // It must JOIN, not quietly open a SECOND battle while the one you
  // meant to join keeps waiting.
  expect(calls[0].url).toBe('/battle/abc123/join')
  expect(JSON.parse(calls[0].body)).toEqual({ team: ['pikachu'] })
  await waitFor(() => expect(loc.href).toBe('/battle/abc123'))
})

test('without ?join the same button opens a new battle', async () => {
  const user = userEvent.setup()
  const loc = { href: '', reload: vi.fn() }
  vi.stubGlobal('location', loc)
  const calls: string[] = []
  vi.stubGlobal('fetch', async (url: string) => {
    calls.push(url)
    return { ok: true, status: 200, json: async () => ({ id: 'new123' }) }
  })

  render(<Pokedex pokemon={dex} types={[]} active="" join="" resume="" />)
  await user.click(screen.getByRole('button', { name: 'Ready to battle' }))

  await waitFor(() => expect(calls[0]).toBe('/battle/open'))
  await waitFor(() => expect(loc.href).toBe('/battle/new123'))
})

// Filtering mid-join must not silently turn a join into an open: the
// type links are the main way to find a Pokemon.
test('type filters keep the battle you are joining', () => {
  render(
    <Pokedex
      pokemon={dex}
      types={[{ name: 'water', count: 3 }]}
      active=""
      join="abc123"
      resume=""
    />,
  )
  expect(screen.getByRole('link', { name: /water/ }))
    .toHaveAttribute('href', '/?type=water&join=abc123')
  expect(screen.getByRole('link', { name: 'all' }))
    .toHaveAttribute('href', '/?join=abc123')
})

// The lobby was unreachable: a .battle-link rule existed in the CSS and
// no element ever used it, so the only way in was typing /battle.
test('the pokedex links to the battle lobby', () => {
  render(<Pokedex pokemon={dex} types={[]} active="" join="" resume="" />)
  expect(screen.getByRole('link', { name: /battle lobby/i }))
    .toHaveAttribute('href', '/battle')
})

// Leaving a battle to look something up is normal; the only way back
// used to be the browser's back button.
test('a battle in progress offers a way back', () => {
  render(<Pokedex pokemon={dex} types={[]} active="" join="" resume="xyz789" />)
  expect(screen.getByRole('link', { name: /back to it/ }))
    .toHaveAttribute('href', '/battle/xyz789')
})

test('the resume link is hidden while joining that same battle', () => {
  render(<Pokedex pokemon={dex} types={[]} active="" join="xyz789" resume="xyz789" />)
  expect(screen.queryByRole('link', { name: /back to it/ })).toBeNull()
})

// Search is client-side because every card is already on the page.
test('search narrows the grid and says how many are left', async () => {
  const user = userEvent.setup()
  render(<Pokedex pokemon={dex} types={[]} active="" join="" resume="" />)

  await user.type(screen.getByLabelText(/search pokemon/i), 'pika')
  expect(screen.getByRole('button', { name: /pikachu/ })).toBeTruthy()
  expect(screen.queryByRole('button', { name: /bulbasaur/ })).toBeNull()
  expect(screen.getByRole('status')).toHaveTextContent('1 pokemon match pika')
})

// A team is three. A fourth pick must not be offered, but deselecting
// has to stay possible or you could trap yourself.
test('a full team blocks new picks but still allows changes', async () => {
  const user = userEvent.setup()
  render(<Pokedex pokemon={[...dex, mon(7, 'squirtle')]} types={[]} active="" join="" resume="" />)

  // The cards, specifically: once a Pokemon is on the team its remove
  // button carries the same name, so a bare getByRole matches two.
  const card = (name: string) =>
    screen.getAllByRole('button', { name: new RegExp(name) })
      .find((el) => el.classList.contains('card'))!

  for (const name of ['pikachu', 'bulbasaur', 'charmander']) {
    await user.click(card(name))
  }
  expect(card('squirtle')).toBeDisabled()
  expect(card('pikachu')).toBeEnabled()
})

// StrictMode double-invokes effects in development, and this app ships
// StrictMode. The team was restored by an effect and written by
// another, so the write ran first with the initial [] and ERASED the
// saved team before the read could restore it: pick three Pokemon,
// reload, and they are gone and overwritten.
//
// Rendering in StrictMode is what catches this class of bug. The rest
// of this file renders plain, which is why the suite was green.
test('a saved team survives a StrictMode mount', () => {
  sessionStorage.setItem(
    'pokedex.team',
    JSON.stringify([{ name: 'pikachu', sprite: 'pikachu.png' }]),
  )
  render(
    <StrictMode>
      <Pokedex pokemon={dex} types={[]} active="" join="" resume="" />
    </StrictMode>,
  )
  const card = screen.getAllByRole('button', { name: /pikachu/ })
    .find((el) => el.classList.contains('card'))!
  expect(card).toHaveAttribute('aria-pressed', 'true')
  expect(JSON.parse(sessionStorage.getItem('pokedex.team') ?? '[]')).toHaveLength(1)
})
