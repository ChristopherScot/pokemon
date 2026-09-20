// The battle lobby: who is waiting, and how to join them.
//
// The list polls, because a battle opened by somebody else after your
// page loaded used to never appear - the list was rendered once and
// frozen at load, so there was no button to join it. Paused while the
// tab is hidden: a lobby left open in a background tab is the shape
// that produced 46,000 wasted requests.
import { useEffect, useRef, useState } from 'react'

import type { components } from '@christopherscot/pokedex-client'

import { V } from './shared.ts'
import { useWaiting } from './useWaiting.ts'

type Waiting = components['schemas']['WaitingBattle']
export type Trainer = { name: string; token: string }

const POLL = 3000

export function Lobby({ me, initial }: { me: Trainer | null; initial: Waiting[] }) {
  const waiting = useWaiting(initial)
  const [renaming, setRenaming] = useState(false)
  const [error, setError] = useState('')

  return (
    <>
      <h1>Battle lobby</h1>
      <p className="sub">
        {me ? <>you are <strong>{me.name}</strong></> : 'pick a trainer name to start'}
        {' · '}<a href="/">back to the pokedex</a>
        {me && <> · <button type="button" className="linkish" onClick={() => setRenaming(true)}>not you?</button></>}
      </p>

      {/* Always reachable, not only when there is no cookie. Trainers
          live in the API's memory, so a deploy invalidates every token
          while the cookie survives - the page then said "you are
          <name>" and every action answered "register first", with the
          only form that could fix it hidden BECAUSE a cookie was
          present. */}
      {(!me || renaming) && <Register onError={setError} />}
      {error && <p className="sub" role="alert">{error}</p>}

      {me && <OpenRow />}

      <h2 style={{ fontSize: 14, color: 'var(--dim)', margin: '22px 0 8px' }}>waiting</h2>
      <div id="waiting">
        {waiting.length === 0
          ? <p className="sub">nobody is waiting. open one below and share the link.</p>
          : waiting.map((w) => (
              <div className="row" key={w.battleId}>
                <div>
                  <strong>{w.trainer}</strong>
                  <div className="sub" style={{ margin: 0 }}>{w.team.join(', ')}</div>
                </div>
                <a className="btn" href={`/?join=${encodeURIComponent(w.battleId)}`}>join</a>
              </div>
            ))}
      </div>
    </>
  )
}

function Register({ onError }: { onError: (m: string) => void }) {
  const [name, setName] = useState('')
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => { input.current?.focus() }, [])

  async function submit() {
    const trimmed = name.trim()
    if (!trimmed) return
    const res = await fetch('/battle/register', {
      method: 'POST',
      headers: { 'content-type': 'application/json', ...V },
      body: JSON.stringify({ name: trimmed }),
    })
    if (res.ok) { location.reload(); return }
    const body = await res.json().catch(() => ({}))
    onError(String(body.message ?? 'that name is taken'))
  }

  return (
    <div className="row" id="reg-row">
      <input
        ref={input}
        id="name"
        aria-label="Trainer name"
        placeholder="trainer name"
        maxLength={32}
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => { if (e.key === 'Enter') void submit() }}
      />
      <button id="reg" type="button" onClick={submit}>register</button>
    </div>
  )
}

function OpenRow() {
  const [busy, setBusy] = useState(false)

  async function open() {
    setBusy(true)
    // No team in the body: this button is explicitly "open with a
    // random team". Choosing one happens in the pokedex, the only
    // screen that can show you what you are choosing between.
    const res = await fetch('/battle/open', {
      method: 'POST',
      headers: { 'content-type': 'application/json', ...V },
      body: JSON.stringify({}),
    })
    const body = await res.json().catch(() => ({}))
    if (res.ok) { location.href = `/battle/${body.id}`; return }
    // A 401 means the cookie's token is one the API no longer knows -
    // the server has already cleared it, so a reload brings back the
    // register form. Alerting and stopping was a dead end.
    if (res.status === 401) { location.reload(); return }
    setBusy(false)
  }

  return (
    <>
      <div className="row">
        <div>
          <strong>your team</strong>
          <div className="sub" style={{ margin: 0 }}>
            pick from the pokedex, or start straight away and get a random team.
          </div>
        </div>
      </div>
      <div className="row">
        <a className="btn" href="/">pick a team →</a>
        <button id="open" type="button" onClick={open} disabled={busy}>
          {busy ? 'opening…' : 'open with a random team'}
        </button>
      </div>
    </>
  )
}
