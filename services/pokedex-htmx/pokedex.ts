import type { FastifyInstance } from 'fastify'
import type { components } from '@christopherscot/pokedex-client'

import { api } from './api.ts'
import { HTMX, MORPH, PAGE_JS } from './assets.ts'
import { POKEDEX_CSS } from './css.ts'
import { colour, esc, TEAM_SIZE } from './types.ts'

type Pokemon = components['schemas']['Pokemon']
type TypeSummary = components['schemas']['TypeSummary']

export type Ctx = {
  pokemon: Pokemon[]
  types: TypeSummary[]
  active: string
  join: string
  team: string[]
  // Sprites for picked names, kept separate from `pokemon` because that list
  // is filtered: a name picked before filtering has no sprite in it.
  sprites: Record<string, string>
  resume: string
}

// Team travels in the querystring so filter links and reloads preserve it.
export function url(
  { active, join, team }: { active?: string; join?: string; team?: string[] },
): string {
  const parts: string[] = []
  if (active) parts.push(`type=${encodeURIComponent(active)}`)
  if (join) parts.push(`join=${encodeURIComponent(join)}`)
  for (const t of team ?? []) parts.push(`team=${encodeURIComponent(t)}`)
  return parts.length ? `/?${parts.join('&')}` : '/'
}

const spriteOf = (ctx: Ctx, name: string): string =>
  ctx.sprites[name] ?? ctx.pokemon.find((p) => p.name === name)?.sprite ?? ''

// The slots and their hidden inputs ARE the team: no server-side session.
// The API only learns about a team at open/join.
export function teamSlots(ctx: Ctx): string {
  const slots = Array.from({ length: TEAM_SIZE }, (_, i) => {
    const name = ctx.team[i]
    if (!name) {
      return `<div class="slot" title="random"><span class="slot-empty">🎲</span></div>`
    }
    return `<div class="slot filled">` +
      `<input type="hidden" name="team" value="${esc(name)}">` +
      `<img src="${esc(spriteOf(ctx, name))}" alt="${esc(name)}">` +
      `<button type="button" class="remove" aria-label="remove ${esc(name)} from your team"` +
      ` hx-post="/team?drop=${encodeURIComponent(name)}"` +
      ` hx-include="#team-form" hx-sync="#team-form:queue all"` +
      ` hx-target="#team" hx-swap="outerHTML"` +
      ` hx-vals='{"active":"${esc(ctx.active)}","join":"${esc(ctx.join)}"}'>×</button>` +
      `</div>`
  }).join('')

  return `<div class="team-slots" id="team">${slots}</div>`
}

export const readyAction = (ctx: Ctx): string =>
  ctx.join ? `/battle/${encodeURIComponent(ctx.join)}/join` : '/battle/open'

export function readyButton(ctx: Ctx): string {
  return `<button id="ready" type="submit" form="team-form" data-join="${esc(ctx.join)}">` +
    `${ctx.join ? 'Join battle' : 'Ready to battle'}</button>`
}

export function card(mon: Pokemon, ctx: Ctx): string {
  const picked = ctx.team.includes(mon.name)
  const full = ctx.team.length >= TEAM_SIZE

  const types = mon.types
    .map((t) => `<span class="type" style="background:${colour(t)}">${esc(t)}</span>`)
    .join('')

  const moves = mon.moves.map((m) =>
    `<li><span>${esc(m.name)}</span>` +
    `<span class="move-type" style="color:${colour(m.type)}">${esc(m.type)}</span>` +
    // Status moves have power 0; render as a dash so it reads as "no damage".
    `<b>${m.power || '—'}</b></li>`).join('')

  // Disabled rather than omitted: the card is also the pokedex entry, and
  // removing it would withdraw information, not just an action.
  const disabled = full && !picked ? ' disabled' : ''

  return `<button type="button" id="card-${esc(mon.name)}"` +
    ` class="card${picked ? ' picked' : ''}" aria-pressed="${picked}"${disabled}` +
    // hx-sync on the FORM: every pick posts the team the form holds at click
    // time, so `queue all` keeps concurrent clicks from racing each other.
    ` hx-post="/team?toggle=${encodeURIComponent(mon.name)}"` +
    ` hx-include="#team-form" hx-sync="#team-form:queue all"` +
    ` hx-target="#team" hx-swap="outerHTML"` +
    ` hx-vals='{"active":"${esc(ctx.active)}","join":"${esc(ctx.join)}"}'>` +
    `<header><span class="num">#${String(mon.id).padStart(3, '0')}</span>` +
    `<h2>${esc(mon.name)}</h2></header>` +
    `<img src="${esc(mon.sprite)}" alt="" loading="lazy" width="96" height="96">` +
    `<div class="types">${types}</div>` +
    `<dl><dt>height</dt><dd>${(mon.height / 10).toFixed(1)} m</dd>` +
    `<dt>weight</dt><dd>${(mon.weight / 10).toFixed(1)} kg</dd></dl>` +
    `<ul class="moves">${moves}</ul>` +
    `</button>`
}

export function grid(ctx: Ctx, oob = false): string {
  // morph rather than default outerHTML: a plain oob swap replaces the
  // element and destroys queued clicks against the cards it removed.
  return `<main class="grid" id="grid"${oob ? ' hx-swap-oob="morph:outerHTML"' : ''}>` +
    `${ctx.pokemon.map((p) => card(p, ctx)).join('')}` +
    `<p class="empty" id="no-match" hidden>nothing matches “<span id="no-match-q"></span>”.</p>` +
    `</main>`
}

export function filters(ctx: Ctx, oob = false): string {
  const link = (q: { active?: string }, label: string, on: boolean, style = '') =>
    `<a href="${url({ ...q, join: ctx.join, team: ctx.team })}"` +
    `${on ? ' class="on"' : ''}${style}>${label}</a>`

  // Each filter is a shareable URL; the join id and team ride along so a
  // filter mid-pick does not drop them.
  return `<nav class="filters" id="filters"${oob ? ' hx-swap-oob="morph:outerHTML"' : ''}>` +
    link({}, 'all', !ctx.active) +
    ctx.types.map((t) =>
      link({ active: t.name }, `${esc(t.name)} <small>${t.count}</small>`,
        ctx.active === t.name, ` style="border-color:${colour(t.name)}"`),
    ).join('') +
    `</nav>`
}

function topbar(ctx: Ctx): string {
  const title = ctx.join ? 'Pick your team' : 'Pokedex'
  const sub = ctx.join
    ? `joining battle <code>${esc(ctx.join)}</code> · pick up to three, or none for a random team · ` +
      `<a class="battle-link" href="/battle">back to the lobby</a>`
    : `${ctx.pokemon.length} pokemon · <a class="battle-link" href="/battle">Battle lobby →</a>`

  // Battle-in-progress link is rendered server-side from the battle cookie.
  const resume = ctx.resume && ctx.resume !== ctx.join
    ? `<p class="sub">you are in battle <code>${esc(ctx.resume)}</code> · ` +
      `<a class="battle-link" href="/battle/${encodeURIComponent(ctx.resume)}">back to it →</a></p>`
    : ''

  return `<header class="topbar" id="topbar">` +
    `<div class="title"><h1>${title}</h1><p class="sub">${sub}</p>${resume}</div>` +
    `<input id="search" type="search" aria-label="Search pokemon by name"` +
    ` placeholder="Search pokemon..." autocomplete="off">` +
    // hx-post, not a native submit: the server answers with HX-Redirect or
    // the name dialog, and a native submit would navigate away instead.
    `<form id="team-form" hx-post="${esc(readyAction(ctx))}"` +
    ` hx-target="#dialog" hx-swap="innerHTML">` +
    `<input type="hidden" name="active" value="${esc(ctx.active)}">` +
    `<input type="hidden" name="join" value="${esc(ctx.join)}">` +
    teamSlots(ctx) + readyButton(ctx) +
    `</form>` +
    `</header>`
}

export function page(ctx: Ctx, error = ''): string {
  return `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Pokedex</title>
<style>${POKEDEX_CSS}</style>
</head><body hx-ext="morph">
${topbar(ctx)}
${filters(ctx)}
<p class="sr-only" role="status" id="count"></p>
${error ? `<p class="sub" role="alert">${esc(error)}</p>` : ''}
${grid(ctx)}
<div id="dialog"></div>
<!-- "probed" is fired, and platform() defined, by client/page.js once
     the hardware probe has an answer to send. -->
<footer id="downloads" hx-get="/downloads" hx-trigger="probed" hx-swap="outerHTML"
        hx-vals="js:{...platform()}" hidden></footer>
<script src="${HTMX.url}"></script>
<script src="${MORPH.url}"></script>
<script src="${PAGE_JS.url}"></script>
</body></html>`
}
