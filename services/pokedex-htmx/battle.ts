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
// How long the float animation runs, in milliseconds. css.ts
// interpolates this so the number lives in one place rather than being
// written here and again as "2.4s" in the stylesheet.
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
// Derived from the battle in hand, with no memory between requests.
//
// This used to keep a Map of when each battle's log first reached each
// length, because the API does not timestamp log entries. That worked,
// but it meant the service could only ever run as ONE copy: a second
// copy would build its own separate record, a browser could be served
// by either, and the same damage number would have two different ages.
// Code that pins replicas to 1 is code with a bug in it.
//
// Every log event carries a turnNumber, so "what just happened" is
// simply "the events from the highest turn number in the log". That is
// a property of the battle, identical on every copy of the service and
// on every request, so any number of replicas agree.
//
// The trade: a float now clears when the next turn lands rather than
// after a fixed 2.4s. In practice a turn takes longer than that, and
// the CSS animation still runs for 2.4s and holds its end state, so
// what a player sees is unchanged. What changes is that a battle left
// idle keeps its last floats faded-out on screen rather than removing
// the elements - which morph handles, because the ids are stable.
export function floatsFor(b: Battle, mineIdx: number): Float[] {
  const out: Float[] = []
  if (b.log.length === 0) return out

  const latest = b.log.reduce((n, e) => (e.turnNumber > n ? e.turnNumber : n), 0)

  b.log.forEach((e, i) => {
    if (e.turnNumber !== latest) return
    if (e.damage === undefined || e.damage === null || !e.target) return
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

// verdict is passed in because the two sides are selected by DIFFERENT
// rules, and using one for both broke targeting completely.
//
// The API publishes them asymmetrically:
//
//   CanAct:        sideActive && c.canAct()
//   CanBeTargeted: !sideActive && c.canBeTargeted()
//
// so on your turn the opponent's side is not active and every one of
// their canAct is false. Reading canAct for them rendered no
// name="target" radio at all - the form posted no target, readTurn
// fell back to 0, and every attack hit their first slot while the
// button said "attack staryu". In a 3-v-3 game that is most of the
// tactics, gone silently.
const sideBlock = (
  title: string, team: BattlePokemon[], side: 'me' | 'them',
  floats: Float[], selectable: boolean, selectedAt: number,
  verdict: (p: BattlePokemon) => boolean,
): string =>
  `<div class="side"><h2>${esc(title)}</h2>` +
  team.map((p, i) => mon(p, side, i, {
    floats,
    // canAct, not !fainted: fainted is the input the server used,
    // and reading it here is a second copy of the rule. usableMoves
    // and canAct are the server's own verdict.
    selectable: selectable && verdict(p),
    selected: selectedAt === i,
    name: p.name,
  })).join('') +
  `</div>`

// The move picker. Note what is NOT here: there is no client-side copy
// of the server's turn rules. pokedex-web shipped an 18-line checkTurn()
// that re-implemented the server's validation so it could grey buttons
// out; the server simply does not render a control you may not use.
//
// That claim used to be almost true - this file still decided a move
// was unusable by comparing against disabledMove, which is the rule
// rather than the verdict. It now reads usableMoves and canAct, which
// the server computes.
function pick(b: Battle, mine: Side, theirs: Side, sel: Turn): string {
  const attacker = mine.team[sel.attacker]
  if (!attacker) return ''

  const moves = attacker.moves.map((m, i) => {
    // A disabled move is not a disabled button: the control is omitted
    // and the reason stated, because "disabled this turn" is real
    // information and a greyed button hides it behind a tooltip.
    // usableMoves is the server's answer, one entry per move. The
    // disabledMove fallback is for a battle from a server that
    // predates the field.
    const usable = attacker.usableMoves ? attacker.usableMoves[i] : attacker.disabledMove !== i
    if (!usable) {
      return `<span class="move-off">${esc(m.name)} <small>disabled this turn</small></span>`
    }
    // form="turn" - the form is in battlePage(), see mon() above.
    return `<label class="movebtn${sel.move === i ? ' sel' : ''}" id="move-${i}">` +
      `<input type="radio" name="move" value="${i}" form="turn" class="sr-only"` +
      `${sel.move === i ? ' checked' : ''}>` +
      `${esc(m.name)} <span style="opacity:.6">${m.power || '—'}</span></label>`
  }).join('')

  const target = theirs.team[sel.target]

  // Every wrapper in here carries an id for the same reason the mon
  // rows do: without one, morph has nothing to match and rebuilds the
  // subtree, which DETACHES the attack button. A player who clicked as
  // a poll landed lost the click and their turn.
  return `<div class="pick" id="pick">` +
    `<strong id="pick-who">${esc(attacker.name)}</strong> uses…` +
    `<div class="moves" id="moves">${moves}</div>` +
    // Its own row, red and right-aligned: this ENDS the turn, and it
    // used to look like a fifth move.
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
      sideBlock(side.trainer, side.team, 'them', floats, false, -1,
        (p) => p.canBeTargeted ?? !p.fainted)).join('') +
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
  { b, me, sel, rejected }:
  { b: Battle; me: string; sel: Turn; rejected: string },
): string {
  const mineIdx = b.sides.findIndex((s) => s.trainer === me)
  const floats = floatsFor(b, mineIdx)
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
    sideBlock(theirs.trainer, theirs.team, 'them', floats, myTurn, sel.target,
      (p) => p.canBeTargeted ?? !p.fainted) +
    sideBlock('you', mine.team, 'me', floats, myTurn, sel.attacker,
      (p) => p.canAct ?? !p.fainted) +
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
