// @vitest-environment jsdom
import { render, screen, waitFor } from '@testing-library/react'
import { userEvent } from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { Lobby } from './Lobby.tsx'

afterEach(() => { vi.unstubAllGlobals() })

const waiting = [
  { battleId: 'xyz789', trainer: 'misty', team: ['staryu', 'psyduck', 'goldeen'] },
] as never

// Joining used to mean typing three Pokemon names from memory into a
// text box. The card grid is the only screen that shows you what you
// are choosing between, so every join goes there.
test('each waiting battle links to the team picker', () => {
  render(<Lobby me={{ name: 'ash', token: 't' }} initial={waiting} />)
  const join = screen.getByRole('link', { name: 'join' })
  expect(join).toHaveAttribute('href', '/?join=xyz789')
})

test('the typed-team input is gone', () => {
  render(<Lobby me={{ name: 'ash', token: 't' }} initial={waiting} />)
  expect(screen.queryByPlaceholderText(/charizard/i)).toBeNull()
})

// Trainers live in the API's memory, so every deploy invalidates every
// token while the browser's cookie survives. The lobby then said "you
// are ash" from the cookie's NAME while every action answered 401 from
// its TOKEN - and the register form was hidden BECAUSE a cookie was
// present. There was no way out but clearing site data.
test('a trainer can always register again', async () => {
  const user = userEvent.setup()
  render(<Lobby me={{ name: 'ash', token: 'stale' }} initial={[]} />)

  expect(screen.queryByLabelText('Trainer name')).toBeNull()
  await user.click(screen.getByRole('button', { name: /not you/i }))
  expect(screen.getByLabelText('Trainer name')).toBeTruthy()
})

test('with no trainer the register form is already showing', () => {
  render(<Lobby me={null} initial={[]} />)
  expect(screen.getByLabelText('Trainer name')).toBeTruthy()
})

// A 401 from "open" means the cookie's token is dead and the server has
// already cleared it, so reloading shows the register form. Alerting
// and stopping left the user staring at a name they could not use.
test('a stale trainer reloads into the register form rather than failing silently', async () => {
  const user = userEvent.setup()
  const reload = vi.fn()
  vi.stubGlobal('location', { href: '', reload })
  vi.stubGlobal('fetch', async () => ({
    ok: false, status: 401, json: async () => ({ message: 'register first' }),
  }))

  render(<Lobby me={{ name: 'ash', token: 'stale' }} initial={[]} />)
  await user.click(screen.getByRole('button', { name: /open with a random team/i }))

  await waitFor(() => expect(reload).toHaveBeenCalled())
})

// Opening a battle sends NO team: this button means "random". Choosing
// a team happens in the pokedex.
test('opening sends an empty body and lands on the new battle', async () => {
  const user = userEvent.setup()
  const loc = { href: '', reload: vi.fn() }
  vi.stubGlobal('location', loc)
  let sent: string | undefined
  vi.stubGlobal('fetch', async (_u: string, init?: { body?: string }) => {
    sent = init?.body
    return { ok: true, status: 200, json: async () => ({ id: 'new123' }) }
  })

  render(<Lobby me={{ name: 'ash', token: 't' }} initial={[]} />)
  await user.click(screen.getByRole('button', { name: /open with a random team/i }))

  await waitFor(() => expect(loc.href).toBe('/battle/new123'))
  expect(JSON.parse(sent ?? '{}')).toEqual({})
})
