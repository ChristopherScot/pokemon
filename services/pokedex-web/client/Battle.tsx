// The battle screen.
//
// The rules it enforces are shared, not reinvented: checkTurn in
// shared.ts mirrors the server's takeTurn and the Go clients'
// battleclient.CheckTurn, in the same ORDER - a client that reports a
// different first reason than the authority teaches a rule that is not
// the rule.
import { useState } from 'react'

import type { components } from '@christopherscot/pokedex-client'

import { MonView } from './Mon.tsx'
import { V, checkTurn, effectBand } from './shared.ts'

type Battle = components['schemas']['Battle']
type Side = components['schemas']['Side']

function sideFor(b: Battle, me: string): { mine: Side; theirs: Side } | null {
  const i = b.sides.findIndex((s) => s.trainer === me)
  // -1 here is the bug that left the board on "loading…" forever:
  // 1 - (-1) is 2, so BOTH sides came back undefined and render threw.
  if (i === -1 || b.sides.length < 2) return null
  return { mine: b.sides[i], theirs: b.sides[1 - i] }
}

export function BattleBoard({ battle: b, me }: { battle: Battle; me: string }) {
  const [picked, setPicked] = useState({ attacker: 0, move: 0, target: 0 })
  const [sending, setSending] = useState(false)
  const [rejected, setRejected] = useState('')

  const sides = sideFor(b, me)
  if (!sides) return <Spectating battle={b} />
  const { mine, theirs } = sides

  const myTurn = b.status === 'active' && b.turn === me
  const attacker = mine.team[picked.attacker]

  async function send() {
    const why = checkTurn(b, me, picked)
    if (why) { setRejected(why); return }
    setSending(true)
    try {
      const res = await fetch(`/battle/${b.id}/turn`, {
        method: 'POST',
        headers: { 'content-type': 'application/json', ...V },
        body: JSON.stringify(picked),
      })
      if (!res.ok) {
        // The state moved on between the check and the send. The server
        // is the authority and this is what it said.
        const body = await res.json().catch(() => ({ message: 'that move was rejected' }))
        setRejected(String(body.message ?? 'that move was rejected'))
      }
    } finally {
      setSending(false)
    }
  }

  return (
    <>
      <Banner battle={b} me={me} myTurn={myTurn} />

      <div className="side">
        <h2>{theirs.trainer}</h2>
        {theirs.team.map((p, i) => (
          <MonView
            key={`${p.name}-${i}`} mon={p} side="them" index={i}
            selectable={myTurn && !p.fainted}
            selected={myTurn && picked.target === i}
            onPick={(t) => setPicked((s) => ({ ...s, target: t }))}
          />
        ))}
      </div>

      <div className="side">
        <h2>you</h2>
        {mine.team.map((p, i) => (
          <MonView
            key={`${p.name}-${i}`} mon={p} side="me" index={i}
            selectable={myTurn && !p.fainted}
            selected={myTurn && picked.attacker === i}
            onPick={(a) => setPicked((s) => ({ ...s, attacker: a, move: 0 }))}
          />
        ))}
      </div>

      {myTurn && attacker && (
        <div className="pick">
          <strong>{attacker.name}</strong> uses…
          <div className="moves">
            {attacker.moves.map((m, i) => (
              <button
                key={m.name}
                type="button"
                data-move={i}
                // The spec says selecting a disabled move is a 409, so
                // a client should show it as unavailable rather than
                // letting the turn fail.
                disabled={attacker.disabledMove === i}
                title={attacker.disabledMove === i ? 'disabled this turn' : undefined}
                className={picked.move === i ? 'sel' : undefined}
                onClick={() => setPicked((s) => ({ ...s, move: i }))}
              >
                {m.name} <span style={{ opacity: 0.6 }}>{m.power || '—'}</span>
              </button>
            ))}
          </div>
          {/* Its own row, red and right-aligned: this ENDS the turn,
              and it used to look like a fifth move. */}
          <div className="commit">
            <button id="go" type="button" onClick={send} disabled={sending}>
              {sending ? 'attacking…' : `attack ${theirs.team[picked.target]?.name ?? ''}`}
            </button>
          </div>
        </div>
      )}

      <Log battle={b} rejected={rejected} />
    </>
  )
}

function Banner({ battle: b, me, myTurn }: { battle: Battle; me: string; myTurn: boolean }) {
  let text: string
  let cls: string
  if (b.status === 'finished') {
    text = b.winner === me ? '★ you win!' : b.winner ? `${b.winner} wins` : 'a draw'
    cls = 'over'
  } else if (b.status === 'waiting') {
    text = 'waiting for an opponent — share the battle id'
    cls = 'theirs'
  } else {
    text = myTurn ? 'your turn' : `waiting on ${b.turn}…`
    cls = myTurn ? 'mine' : 'theirs'
  }
  // role=status so a screen-reader user is TOLD their turn began. The
  // banner used to be rebuilt inside the board's innerHTML, and a live
  // region that is destroyed and recreated never announces.
  return <div className={`banner ${cls}`} role="status" aria-live="polite" aria-atomic="true">{text}</div>
}

function Log({ battle: b, rejected }: { battle: Battle; rejected: string }) {
  return (
    <div className="log" id="log">
      {b.log.slice(-8).map((e, i) => {
        const band = effectBand(e.effectiveness)
        return <div key={i} className={band === 'normal' ? undefined : band}>{e.text}</div>
      })}
      {rejected && <div className="weak">{rejected}</div>}
    </div>
  )
}

function Spectating({ battle: b }: { battle: Battle }) {
  const joinable = b.status === 'waiting' && b.sides.length < 2
  return (
    <>
      <div className="banner theirs" role="status" aria-live="polite">
        {joinable
          ? 'this battle is waiting for an opponent'
          : `watching ${b.sides.map((s) => s.trainer).join(' vs ')}`}
      </div>
      {b.sides.map((side) => (
        <div className="side" key={side.trainer}>
          <h2>{side.trainer}</h2>
          {side.team.map((p, i) => (
            <MonView key={`${p.name}-${i}`} mon={p} side="them" index={i}
                     selectable={false} selected={false} onPick={() => {}} />
          ))}
        </div>
      ))}
      {joinable && (
        <div className="pick">
          <a className="btn" href={`/?join=${encodeURIComponent(b.id)}`}>pick your team →</a>
        </div>
      )}
    </>
  )
}
