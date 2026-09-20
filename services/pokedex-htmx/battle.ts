import type { components } from '@christopherscot/pokedex-client'

import { HTMX, MORPH } from './assets.ts'
import { BATTLE_CSS } from './css.ts'
import { colour, esc } from './types.ts'

type Battle = components['schemas']['Battle']
type Side = components['schemas']['Side']
type BattlePokemon = components['schemas']['BattlePokemon']

// A float lives 2400ms, matching `animation:floatUp 2.4s` in the CSS.
// pokedex-web kept this clock in the browser; here the server owns it
// and renders the floats that are still alive on each poll. Morph
// preserves a float whose id and attributes have not changed, so the
// animation runs uninterrupted across ticks rather than restarting.
export const LIFE_MS = 2400

export const effectBand = (e: number | null | undefined): string => {
  if (e === undefined || e === null) return 'normal'
  if (e === 0) return 'immune'
  if (e >= 2) return 'super'
  if (e < 1) return 'weak'
  return 'normal'
}

export type Float = { id: string; slot: string; text: string; band: string }

// Which floats are still on screen.
//
// The API's log entries carry no timestamp - only a turnNumber - so age
// cannot be read off the battle. What the API does give is `version`,
// which increases on every change. This BFF therefore keeps one small
// record per battle: the log length it first saw at each version, and
// the wall clock when it saw it. A log entry's birth time is the time of
// the earliest version whose log was already that long.
//
// It matters that this is IDEMPOTENT. Two browsers poll the same battle
// a few hundred ms apart, and a spectator may join at any point; a
// function that mutated state per call would let one poll cut another's
// float short. Observing a version is the only write, and it is
// write-once.
type Seen = { at: number; logLength: number }

const seen = new Map<string, Seen[]>()

export function observe(b: Battle, now = Date.now()): void {
  let history = seen.get(b.id) ?? []

  // A log that SHRANK is not this battle's log any more: the API keeps
  // battles in memory, so a restart - or a reused id - starts a new one.
  // Without this the old timestamps are kept, every entry reads as older
  // than a float lifetime, and the new battle renders no floats at all.
  if (history.length && history[history.length - 1].logLength > b.log.length) {
    history = []
  }

  if (!history.some((h) => h.logLength >= b.log.length)) {
    history.push({ at: now, logLength: b.log.length })
    // Bounded by TIME, not by count. A turn appends two or three entries,
    // so a fixed 16 was about six turns - easily inside 2.4s with two
    // quick players, and dropping an entry that is still needed pushes a
    // float's birth time FORWARD, leaving it on screen after its
    // animation has finished.
    while (history.length > 1 && now - history[0].at > LIFE_MS) history.shift()
    seen.set(b.id, history)
  }
  if (seen.size > 500) {
    for (const [id, h] of seen) {
      if (now - h[h.length - 1].at > LIFE_MS * 10) seen.delete(id)
    }
  }
}

// When the log first reached length i+1, i.e. when entry i appeared.
function bornAt(battleId: string, i: number, now: number): number {
  const history = seen.get(battleId) ?? []
  for (const h of history) if (h.logLength >= i + 1) return h.at
  return now
}

export function floatsFor(b: Battle, mineIdx: number, now = Date.now()): Float[] {
  const out: Float[] = []
  b.log.forEach((e, i) => {
    if (e.damage === undefined || e.damage === null || !e.target) return
    if (now - bornAt(b.id, i, now) >= LIFE_MS) return
    for (const [si, side] of b.sides.entries()) {
      const at = side.team.findIndex((p) => p.name === e.target)
      if (at < 0) continue
      const band = effectBand(e.effectiveness)
      const slot = `${si === mineIdx ? 'me' : 'them'}-${at}`
      out.push({
        id: `f-${i}-${slot}`,
        slot,
        // An immune hit deals 0, so "-0" says nothing. Name it.
        text: band === 'immune' ? 'no effect' : `-${e.damage}${band === 'super' ? ' !!' : ''}`,
        band,
      })
    }
  })
  return out
}

const hpColour = (hp: number, max: number): string => {
  const f = max > 0 ? hp / max : 0
  return f <= 0.2 ? 'var(--danger)' : f <= 0.5 ? 'var(--warn)' : 'var(--good)'
}

// One team member. Every element that animates or holds state carries a
// STABLE ID: that is what lets idiomorph match it across a swap and
// mutate it in place instead of replacing it. Without the ids the HP
// bar renders already-drained and every float restarts each second.
export function mon(
  p: BattlePokemon, side: 'me' | 'them', i: number,
  { floats, selectable, selected, name }:
  { floats: Float[]; selectable: boolean; selected: boolean; name?: string },
): string {
  const slot = `${side}-${i}`
  const pct = p.maxHp > 0 ? Math.max(0, (p.hp / p.maxHp) * 100) : 0
  const mine = floats.filter((f) => f.slot === slot)

  // The shake fires when a float is live on this slot. Morph only
  // touches `class` when the value actually changes, so it fires once
  // on arrival and does not re-trigger while the float persists.
  const hit = mine.some((f) => f.band !== 'immune')
  const hard = mine.some((f) => f.band === 'super')

  const types = p.types
    .map((t) => `<span class="type" style="background:${colour(t)}">${esc(t)}</span>`).join('')

  const inner =
    mine.map((f) => `<div class="float ${f.band}" id="${f.id}">${esc(f.text)}</div>`).join('') +
    `<img src="${esc(p.sprite)}" alt="" loading="lazy">` +
    `<div><div class="name">${esc(p.name)}</div>` +
    `<div class="types">${types}</div>` +
    `<div class="hpwrap"><div class="hp" id="hp-${slot}"` +
    ` style="width:${pct}%;background:${hpColour(p.hp, p.maxHp)}"></div></div></div>` +
    `<div class="hpnum" id="hpnum-${slot}">${p.hp}/${p.maxHp}</div>`

  const cls = 'mon' + (p.fainted ? ' fainted' : '') + (hard ? ' hit-hard' : hit ? ' hit' : '')

  if (!selectable) {
    return `<div class="${cls}" id="mon-${slot}" data-slot="${slot}">${inner}</div>`
  }
  // A real radio, so the selection is part of the form that posts the
  // turn. `checked` is rendered by the SERVER, which already owns turn
  // state - idiomorph forces a live checked property to match the
  // markup, so a client-only selection would be clobbered each poll.
  //
  // form="turn" names a form that is NOT an ancestor: it lives in
  // battlePage(), outside the polled region, so the poll cannot rebuild
  // it mid-choice. HTML allows the association by id.
  const field = side === 'me' ? 'attacker' : 'target'
  return `<label class="${cls}" id="mon-${slot}" data-slot="${slot}"` +
    `${selected ? ' style="outline:2px solid #6d5ae0"' : ''}>` +
    `<input type="radio" name="${field}" value="${i}" id="pick-${slot}" class="sr-only"` +
    ` form="turn"${selected ? ' checked' : ''}` +
    `${name ? ` aria-label="${esc(name)}"` : ''}>${inner}</label>`
}

function banner(b: Battle, me: string, myTurn: boolean): string {
  let text: string, cls: string
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
  // One node with a stable id, so morph mutates the text in place. A
  // replaced live region announces nothing, which was a real regression
  // in the version before pokedex-web.
  return `<div class="banner ${cls}" id="banner" role="status" aria-live="polite"` +
    ` aria-atomic="true">${esc(text)}</div>`
}

function log(b: Battle, rejected: string): string {
  const lines = b.log.slice(-8).map((e) => {
    const band = effectBand(e.effectiveness)
    return `<div${band === 'normal' ? '' : ` class="${band}"`}>${esc(e.text)}</div>`
  }).join('')
  return `<div class="log" id="log">${lines}` +
    `${rejected ? `<div class="weak">${esc(rejected)}</div>` : ''}</div>`
}

const sideBlock = (
  title: string, team: BattlePokemon[], side: 'me' | 'them',
  floats: Float[], selectable: boolean, selectedAt: number,
): string =>
  `<div class="side"><h2>${esc(title)}</h2>` +
  team.map((p, i) => mon(p, side, i, {
    floats,
    selectable: selectable && !p.fainted,
    selected: selectedAt === i,
    name: p.name,
  })).join('') +
  `</div>`

// The move picker. Note what is NOT here: there is no client-side copy
// of the server's turn rules. pokedex-web shipped an 18-line checkTurn()
// that re-implemented the server's validation so it could grey buttons
// out; the server simply does not render a control you may not use.
function pick(b: Battle, mine: Side, theirs: Side, sel: Turn): string {
  const attacker = mine.team[sel.attacker]
  if (!attacker) return ''

  const moves = attacker.moves.map((m, i) => {
    // A disabled move is not a disabled button: the control is omitted
    // and the reason stated, because "disabled this turn" is real
    // information and a greyed button hides it behind a tooltip.
    if (attacker.disabledMove === i) {
      return `<span class="move-off">${esc(m.name)} <small>disabled this turn</small></span>`
    }
    // form="turn" - the form is in battlePage(), see mon() above.
    return `<label class="movebtn${sel.move === i ? ' sel' : ''}" id="move-${i}">` +
      `<input type="radio" name="move" value="${i}" form="turn" class="sr-only"` +
      `${sel.move === i ? ' checked' : ''}>` +
      `${esc(m.name)} <span style="opacity:.6">${m.power || '—'}</span></label>`
  }).join('')

  const target = theirs.team[sel.target]

  return `<div class="pick" id="pick">` +
    `<strong>${esc(attacker.name)}</strong> uses…` +
    `<div class="moves">${moves}</div>` +
    // Its own row, red and right-aligned: this ENDS the turn, and it
    // used to look like a fifth move.
    `<div class="commit"><button id="go" type="submit" form="turn">` +
    `attack ${esc(target?.name ?? '')}</button></div>` +
    `</div>`
}

export type Turn = { attacker: number; move: number; target: number }

export const sideFor = (b: Battle, me: string): { mine: Side; theirs: Side } | null => {
  const i = b.sides.findIndex((s) => s.trainer === me)
  if (i === -1 || b.sides.length < 2) return null
  return { mine: b.sides[i], theirs: b.sides[1 - i] }
}

// A spectator gets a different board, not a disabled one: both sides are
// rendered as "them", there is no move picker, and if the battle is
// still open they get a link to join it.
function spectating(b: Battle, floats: Float[]): string {
  const joinable = b.status === 'waiting' && b.sides.length < 2
  const head = joinable
    ? 'this battle is waiting for an opponent'
    : `watching ${b.sides.map((s) => s.trainer).join(' vs ')}`

  return `<div class="banner theirs" id="banner" role="status" aria-live="polite">${esc(head)}</div>` +
    b.sides.map((side) =>
      sideBlock(side.trainer, side.team, 'them', floats, false, -1)).join('') +
    (joinable
      ? `<div class="pick" id="pick"><a class="btn" href="/?join=${encodeURIComponent(b.id)}">` +
        `pick your team →</a></div>`
      : '')
}

// The polling control lives INSIDE the fragment it swaps, so a board
// that should stop polling simply comes back without it. That replaces
// pokedex-web's client-side rules - five 404s in a row, and a set of
// terminal status codes - with the absence of a control.
// hx-include sends the turn form with every poll, so the selection the
// user has made comes BACK to the server and is rendered as `checked`.
//
// Without it the board is correct but the picker is not: idiomorph forces
// a live `checked` property to match the markup it is given, so a
// client-only selection is reset on the next tick. Server-authoritative
// checked is also the right answer - the server already owns turn state -
// but it only works if the server is told what was picked.
const POLL =
  ` hx-get="{url}" hx-trigger="every 1s [!document.hidden]"` +
  ` hx-sync="this:replace" hx-swap="morph:{ignoreActiveValue:true}"` +
  ` hx-include="#turn" hx-target="this"`

export function board(
  { b, me, sel, rejected, now }:
  { b: Battle; me: string; sel: Turn; rejected: string; now?: number },
): string {
  observe(b, now)
  const mineIdx = b.sides.findIndex((s) => s.trainer === me)
  const floats = floatsFor(b, mineIdx, now)
  const sides = sideFor(b, me)

  const poll = b.status === 'finished'
    ? ''
    : POLL.replace('{url}', `/battle/${encodeURIComponent(b.id)}/board`)

  const won = b.status === 'finished' && b.winner === me
  const open = `<div class="board${won ? ' won' : ''}" id="board" hx-ext="morph"${poll}>`

  if (!sides) return open + spectating(b, floats) + `</div>`

  const { mine, theirs } = sides
  const myTurn = b.status === 'active' && b.turn === me

  return open +
    banner(b, me, myTurn) +
    sideBlock(theirs.trainer, theirs.team, 'them', floats, myTurn, sel.target) +
    sideBlock('you', mine.team, 'me', floats, myTurn, sel.attacker) +
    (myTurn ? pick(b, mine, theirs, sel) : '') +
    log(b, rejected) +
    `</div>`
}

// The turn form lives in the PAGE, not in the board fragment, so the 1s
// poll never rebuilds it. The radios inside the board reference it by
// `form="turn"`, which HTML allows across the document.
export function battlePage(
  { id, trainer, first }: { id: string; trainer: string; first: string },
): string {
  return `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Battle · Pokedex</title>
<style>${BATTLE_CSS}</style>
</head>
<body>
  <h1>Battle</h1>
  <p class="sub">you are <strong>${esc(trainer)}</strong> · battle <code>${esc(id)}</code> ·
     <a href="/">back to the pokedex</a></p>
  <form id="turn" hx-post="/battle/${encodeURIComponent(id)}/turn"
        hx-target="#board" hx-swap="morph:{ignoreActiveValue:true}"></form>
  ${first}
<script src="${HTMX.url}"></script>
<script src="${MORPH.url}"></script>
</body></html>`
}

// The board a battle that is gone comes back as. No hx-trigger, so the
// poll stops because the control is absent rather than because a client
// counted failures.
export const goneBoard = (message: string): string =>
  `<div class="board" id="board">` +
  `<div class="banner over" id="banner" role="status">${esc(message)}</div>` +
  `<p class="sub"><a href="/battle">back to the lobby</a></p></div>`
