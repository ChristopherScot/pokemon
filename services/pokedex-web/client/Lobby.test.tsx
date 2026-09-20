// @vitest-environment jsdom
import { render, screen, waitFor } from '@testing-library/react'
import { userEvent } from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { Lobby } from './Lobby.tsx'

afterEach(() => { vi.unstubAllGlobals() })

const waiting = [
  { battleId: 'xyz789', trainer: 'misty', team: ['staryu', 'psyduck', 'goldeen'] },
] as never

test('each waiting battle links to the team picker', () => {
  render(<Lobby me={{ name: 'ash', token: 't' }} initial={waiting} />)
  const join = screen.getByRole('link', { name: 'join' })
  expect(join).toHaveAttribute('href', '/?join=xyz789')
})

test('the typed-team input is gone', () => {
  render(<Lobby me={{ name: 'ash', token: 't' }} initial={waiting} />)
  expect(screen.queryByPlaceholderText(/charizard/i)).toBeNull()
})

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
