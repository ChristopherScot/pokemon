import type { components } from '@christopherscot/pokedex-client'

import { HTMX, MORPH } from './assets.ts'
import { BATTLE_CSS } from './css.ts'
import { colour, esc } from './types.ts'

type Battle = components['schemas']['Battle']
type Side = components['schemas']['Side']
type BattlePokemon = components['schemas']['BattlePokemon']

// Must match `animation:floatUp 2.4s` in the CSS: morph preserves a float
// with a stable id, so the animation runs uninterrupted across polls.
export const LIFE_MS = 2400

export const effectBand = (e: number | null | undefined): string => {
  if (e === undefined || e === null) return 'normal'
  if (e === 0) return 'immune'
  if (e >= 2) return 'super'
  if (e < 1) return 'weak'
  return 'normal'
}

export type Float = { id: string; slot: string; text: string; band: string }

// Log entries carry no timestamp, so birth time is derived from the earliest
// poll that saw the log at length i+1. Observing is write-once and idempotent:
// two viewers polling must not cut each other's floats short.
type Seen = { at: number; logLength: number }

const seen = new Map<string, Seen[]>()

export function observe(b: Battle, now = Date.now()): void {
  let history = seen.get(b.id) ?? []

  // A shrunk log means the battle id was reused; keeping old timestamps
  // would make every entry read as older than LIFE_MS.
  if (history.length && history[history.length - 1].logLength > b.log.length) {
    history = []
  }

  if (!history.some((h) => h.logLength >= b.log.length)) {
    history.push({ at: now, logLength: b.log.length })
    // Bounded by time, not count: dropping a still-needed entry pushes its
    // birth time forward and leaves the float on screen past its animation.
    while (history.length > 1 && now - history[0].at > LIFE_MS) history.shift()
    seen.set(b.id, history)
  }
  if (seen.size > 500) {
    for (const [id, h] of seen) {
      if (now - h[h.length - 1].at > LIFE_MS * 10) seen.delete(id)
    }
  }
}

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

// Every element that animates or holds state carries a stable id, so idiomorph
// matches it across a swap and mutates in place rather than replacing.
export function mon(
  p: BattlePokemon, side: 'me' | 'them', i: number,
  { floats, selectable, selected, name }:
  { floats: Float[]; selectable: boolean; selected: boolean; name?: string },
): string {
  const slot = `${side}-${i}`
  const pct = p.maxHp > 0 ? Math.max(0, (p.hp / p.maxHp) * 100) : 0
  const mine = floats.filter((f) => f.slot === slot)

  // Morph only touches `class` when the value changes, so the shake fires
  // once on arrival and does not re-trigger while the float persists.
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
  // Server-rendered `checked`: idiomorph forces a live checked property to
  // match markup, so a client-only selection would be clobbered each poll.
  // form="turn" points at the form in battlePage(), outside the polled region.
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
  // Stable id so morph mutates in place: a replaced live region announces nothing.
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
    // canAct is the server's verdict; !fainted would be re-deriving the rule.
    selectable: selectable && (p.canAct ?? !p.fainted),
    selected: selectedAt === i,
    name: p.name,
  })).join('') +
  `</div>`

// HATEOAS: the server does not render a control the trainer may not use, so
// there is no client-side copy of the turn rules to drift from them.
function pick(b: Battle, mine: Side, theirs: Side, sel: Turn): string {
  const attacker = mine.team[sel.attacker]
  if (!attacker) return ''

  const moves = attacker.moves.map((m, i) => {
    // A disabled move is omitted (with the reason stated), not greyed:
    // "disabled this turn" is information a tooltip would hide.
    // disabledMove is a fallback for a battle from a pre-usableMoves server.
    const usable = attacker.usableMoves ? attacker.usableMoves[i] : attacker.disabledMove !== i
    if (!usable) {
      return `<span class="move-off">${esc(m.name)} <small>disabled this turn</small></span>`
    }
    return `<label class="movebtn${sel.move === i ? ' sel' : ''}" id="move-${i}">` +
      `<input type="radio" name="move" value="${i}" form="turn" class="sr-only"` +
      `${sel.move === i ? ' checked' : ''}>` +
      `${esc(m.name)} <span style="opacity:.6">${m.power || '—'}</span></label>`
  }).join('')

  const target = theirs.team[sel.target]

  // Every wrapper needs an id or morph rebuilds the subtree and detaches the
  // attack button; a click landing mid-poll would then be lost.
  return `<div class="pick" id="pick">` +
    `<strong id="pick-who">${esc(attacker.name)}</strong> uses…` +
    `<div class="moves" id="moves">${moves}</div>` +
    `<div class="commit" id="commit"><button id="go" type="submit" form="turn">` +
    `attack ${esc(target?.name ?? '')}</button></div>` +
    `</div>`
}

export type Turn = { attacker: number; move: number; target: number }

export const sideFor = (b: Battle, me: string): { mine: Side; theirs: Side } | null => {
  const i = b.sides.findIndex((s) => s.trainer === me)
  if (i === -1 || b.sides.length < 2) return null
  return { mine: b.sides[i], theirs: b.sides[1 - i] }
}

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

// Polling control lives inside the fragment it swaps, so a board that should
// stop polling simply comes back without it. hx-include sends the turn form
// each poll so the selection round-trips: morph forces `checked` to match
// server markup, so client-only state would be reset. hx-sync=this:replace
// drops an in-flight poll when the next tick fires, so slow responses do not
// pile up. ignoreActiveValue keeps morph from clobbering the value the user
// is typing into a focused input.
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

// The turn form lives in the page, not the polled board fragment. The radios
// inside the board reference it by `form="turn"` (HTML allows cross-document).
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

// No hx-trigger, so polling stops via absence-of-control, not a client counter.
export const goneBoard = (message: string): string =>
  `<div class="board" id="board">` +
  `<div class="banner over" id="banner" role="status">${esc(message)}</div>` +
  `<p class="sub"><a href="/battle">back to the lobby</a></p></div>`
