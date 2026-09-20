import type { components } from '@christopherscot/pokedex-client'

import { HTMX, MORPH } from './assets.ts'
import { LOBBY_CSS } from './css.ts'
import { esc } from './types.ts'

type WaitingBattle = components['schemas']['WaitingBattle']
export type Trainer = { name: string; token: string }

// The waiting list, swapped on its own every 3s. This is the case htmx
// is straightforwardly best at: a list with no local state. Each row
// carries a stable id so morph leaves an unchanged row alone and its
// fadeIn does not re-run on every poll - pokedex-web got that from
// React's keying.
export function waitingList(waiting: WaitingBattle[]): string {
  const rows = waiting.length === 0
    ? `<p class="sub">nobody is waiting. open one below and share the link.</p>`
    : waiting.map((w) =>
        `<div class="row" id="w-${esc(w.battleId)}">` +
        `<div><strong>${esc(w.trainer)}</strong>` +
        `<div class="sub" style="margin:0">${esc(w.team.join(', '))}</div></div>` +
        `<a class="btn" href="/?join=${encodeURIComponent(w.battleId)}">join</a>` +
        `</div>`).join('')

  return `<div id="waiting" hx-get="/battle/waiting" hx-trigger="every 3s [!document.hidden]"` +
    ` hx-sync="this:replace" hx-target="this" hx-swap="morph:innerHTML">${rows}</div>`
}

function register(): string {
  return `<form class="row" id="reg-row" hx-post="/battle/register" hx-target="#reg-row"
        hx-swap="outerHTML">
    <input id="name" name="name" aria-label="Trainer name" placeholder="trainer name"
           maxlength="32" autofocus required>
    <button id="reg" type="submit">register</button>
  </form>`
}

export function lobbyPage(
  { me, waiting, error = '', renaming = false }:
  { me: Trainer | null; waiting: WaitingBattle[]; error?: string; renaming?: boolean },
): string {
  // Always reachable, not only when there is no cookie. Trainers live in
  // the API's memory, so a deploy invalidates every token while the
  // cookie survives - the page then said "you are <name>" and every
  // action answered "register first", with the only form that could fix
  // it hidden BECAUSE a cookie was present.
  const who = me
    ? `you are <strong>${esc(me.name)}</strong> · <a href="/">back to the pokedex</a> · ` +
      `<button type="button" class="linkish" hx-get="/battle/rename" ` +
      `hx-target="#reg-slot" hx-swap="innerHTML">not you?</button>`
    : `pick a trainer name to start · <a href="/">back to the pokedex</a>`

  const openRows = me
    ? `<div class="row"><div><strong>your team</strong>` +
      `<div class="sub" style="margin:0">pick from the pokedex, or start straight away ` +
      `and get a random team.</div></div></div>` +
      `<div class="row"><a class="btn" href="/">pick a team →</a>` +
      `<form hx-post="/battle/open" hx-swap="none" style="display:contents">` +
      `<button id="open" type="submit">open with a random team</button></form></div>`
    : ''

  return `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Battle lobby · Pokedex</title>
<style>${LOBBY_CSS}</style></head>
<body hx-ext="morph">
  <h1>Battle lobby</h1>
  <p class="sub">${who}</p>
  <div id="reg-slot">${!me || renaming ? register() : ''}</div>
  ${error ? `<p class="sub" role="alert">${esc(error)}</p>` : ''}
  ${openRows}
  <h2 style="font-size:14px;color:var(--dim);margin:22px 0 8px">waiting</h2>
  ${waitingList(waiting)}
<script src="${HTMX.url}"></script>
<script src="${MORPH.url}"></script>
</body></html>`
}

export { register as registerRow }
