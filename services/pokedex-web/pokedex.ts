// The card UI, backed by the pokedex service's GENERATED TypeScript
// client.
//
// This service knows no URLs and no response shapes. `npm install
// @christopherscot/pokedex-client` is the whole integration: the package
// is generated from the same openapi.yml the Go server implements, so
// the API has exactly one definition and two consumers derived from it.
//
// Rendered server-side as plain HTML. A card grid over 100 rows does not
// need a framework, a build step or a client bundle, and not having one
// means the container is `node server.js` with no compile stage.

import type { FastifyInstance } from 'fastify'
import createClient, { exponentialRetry, noRetry } from '@christopherscot/pokedex-client'

// The wire types, from the generated client. A field that changes in
// openapi.yml breaks this file at typecheck rather than at runtime.
import type { components } from '@christopherscot/pokedex-client'

type Pokemon = components['schemas']['Pokemon']
type TypeSummary = components['schemas']['TypeSummary']
type Move = components['schemas']['Move']

// Where the API lives. The in-cluster address is the default so the
// deployment needs no configuration; $POKEDEX_URL overrides it for a
// port-forward or a local run.
const baseUrl = process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local'

// exponentialRetry rather than noRetry: this is a read-only UI in front
// of a rolling deployment, and a request that lands mid-rollout should
// wait rather than show the visitor an error page.
// exponentialRetry spends about 3.4s across five attempts before giving
// up, which is right in front of a rolling deployment and wrong in a
// test suite with no API behind it - four routes times five attempts is
// a minute of waiting to assert a 502. POKEDEX_NO_RETRY turns it off.
const api = createClient({
  baseUrl,
  policy: process.env.POKEDEX_NO_RETRY ? noRetry : exponentialRetry,
})

// One colour per type, so a card is scannable without reading it. These
// are the familiar Pokedex colours; anything unknown falls back to grey
// rather than throwing.
const TYPE_COLOURS = {
  normal: '#9fa19f', fire: '#e62829', water: '#2980ef', electric: '#fac000',
  grass: '#3fa129', ice: '#3dcef3', fighting: '#ff8000', poison: '#9141cb',
  ground: '#915121', flying: '#81b9ef', psychic: '#ef4179', bug: '#91a119',
  rock: '#afa981', ghost: '#704170', dragon: '#5060e1', dark: '#50413f',
  steel: '#60a1b8', fairy: '#ef70ef',
}

const colour = (type: string): string =>
  TYPE_COLOURS[type as keyof typeof TYPE_COLOURS] ?? '#6b7280'

// escape() runs on every string that reaches the page. The data is ours
// and the names are tame, but a renderer that only escapes "untrusted"
// input is one dataset change away from not escaping at all.
const escape = (s: unknown): string =>
  String(s).replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] ?? c)

function card(p: Pokemon) {
  const types = p.types
    .map((t) => `<span class="type" style="background:${colour(t)}">${escape(t)}</span>`)
    .join('')

  // Status moves have power 0, which reads as a bug rather than as "deals
  // no damage" - show a dash, the same choice the CLI makes.
  const moves = p.moves
    .map((m) => `<li><span>${escape(m.name)}</span><span class="move-type" style="color:${colour(m.type)}">${escape(m.type)}</span><b>${m.power || '—'}</b></li>`)
    .join('')

  return `
    <article class="card" data-name="${escape(p.name)}" data-sprite="${escape(p.sprite)}">
      <header>
        <span class="num">#${String(p.id).padStart(3, '0')}</span>
        <h2>${escape(p.name)}</h2>
      </header>
      <img src="${escape(p.sprite)}" alt="${escape(p.name)}" loading="lazy" width="96" height="96">
      <div class="types">${types}</div>
      <dl>
        <dt>height</dt><dd>${(p.height / 10).toFixed(1)} m</dd>
        <dt>weight</dt><dd>${(p.weight / 10).toFixed(1)} kg</dd>
      </dl>
      <ul class="moves">${moves}</ul>
    </article>`
}

// Exported like battlePage/lobbyPage so a test can assert on the real
// markup. The route itself 502s without a live API behind it, so
// testing through inject() cannot see the page at all.
export function page({ pokemon, types, active }: { pokemon: Pokemon[]; types: TypeSummary[]; active: string }) {
  const filters = [
    `<a href="/" class="${active ? '' : 'on'}">all</a>`,
    ...types.map((t) =>
      `<a href="/?type=${encodeURIComponent(t.name)}" class="${active === t.name ? 'on' : ''}" style="border-color:${colour(t.name)}">${escape(t.name)} <small>${t.count}</small></a>`),
  ].join('')

  return `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Pokedex</title>
<style>
  :root { color-scheme: dark; --bg:#12141c; --card:#1c1f2b; --fg:#e8eaf2; --dim:#8b91a7; }
  * { box-sizing: border-box; }
  body { margin:0; padding:24px; background:var(--bg); color:var(--fg);
         font:15px/1.5 ui-sans-serif,system-ui,-apple-system,sans-serif; }
  h1 { margin:0 0 4px; font-size:22px; letter-spacing:.02em; }
  .sub { color:var(--dim); margin:0 0 20px; font-size:13px; }
  .battle-link { color:#a78bfa; text-decoration:none; font-weight:600; }
  .battle-link:hover { text-decoration:underline; }

  /* One row: title, slots, button. Sticky, because the grid is 100
     cards long and the slots have to stay visible while you scroll to
     find the third. */
  .topbar { position:sticky; top:0; z-index:10; background:var(--bg);
            padding:16px 0 12px; margin-bottom:6px;
            border-bottom:1px solid #262a38;
            display:flex; flex-wrap:wrap; gap:16px; align-items:center; }
  .topbar h1 { margin:0; }
  .topbar .sub { margin:2px 0 0; }
  /* Pushes the slots and button to the right, and keeps them together
     when the row wraps on a narrow screen. */
  .topbar .title { margin-right:auto; }
  #search { flex:0 1 280px; font:inherit; color:var(--fg); background:var(--card);
            border:1px solid #333a4d; border-radius:999px; padding:8px 14px;
            outline:none; transition:border-color .15s; }
  #search:focus { border-color:#6d5ae0; }
  #search::placeholder { color:#5b6272; }
  /* Downloads. Hidden until the script confirms an asset exists, so a
     release that has not happened shows nothing rather than a dead
     link - see the fetch below. */
  #downloads { margin:48px auto 8px; max-width:640px; padding:24px;
               border:1px solid #2a3040; border-radius:14px; background:var(--card); }
  #downloads h2 { margin:0 0 4px; font-size:16px; }
  #downloads .sub { margin:0 0 16px; }
  .dl-row { display:flex; flex-wrap:wrap; gap:12px; }
  .dl { display:flex; align-items:center; gap:10px; flex:1 1 200px;
        padding:12px 14px; border:1px solid #333a4d; border-radius:10px;
        background:#161926; color:var(--fg); text-decoration:none; }
  .dl:hover { border-color:#4b5573; background:#1a1e2d; }
  .dl svg { flex:0 0 auto; width:22px; height:22px; }
  .dl-name { font-weight:600; }
  .dl-meta { color:var(--dim); font-size:12px; }
  .dl-other { margin:16px 0 0; font-size:13px; }
  .dl-other a { color:var(--dim); }

  /* A card hidden by search is out of the layout, so the grid closes up
     rather than leaving holes. */
  .card.hidden { display:none; }

  dialog { border:1px solid #333a4d; border-radius:14px; background:var(--card);
           color:var(--fg); padding:22px 24px; max-width:380px; width:calc(100% - 32px); }
  dialog::backdrop { background:rgba(9,10,14,.7); backdrop-filter:blur(2px); }
  dialog h2 { margin:0 0 6px; font-size:18px; }
  dialog p { margin:0 0 14px; color:var(--dim); font-size:13px; }
  dialog input { width:100%; font:inherit; color:var(--fg); background:var(--bg);
                 border:1px solid #333a4d; border-radius:8px; padding:9px 12px;
                 outline:none; margin-bottom:14px; }
  dialog input:focus { border-color:#6d5ae0; }
  .modal-error { color:#f87171; margin:-8px 0 12px; }
  .modal-actions { display:flex; gap:8px; justify-content:flex-end; }
  .modal-actions button { font:inherit; font-weight:600; border:none; border-radius:8px;
                          padding:8px 14px; cursor:pointer; color:#12141c; background:#a78bfa; }
  .modal-actions .ghost { background:transparent; color:var(--dim); border:1px solid #333a4d; }
  .team-slots { display:flex; gap:8px; }
  .slot { width:60px; height:60px; border-radius:10px; background:var(--card);
          border:2px dashed #363c4e; display:flex; align-items:center;
          justify-content:center; position:relative; transition:border-color .2s, transform .2s; }
  .slot.filled { border-style:solid; border-color:#6d5ae0; }
  .slot img { width:52px; height:52px; image-rendering:pixelated; }
  .slot-empty { color:#4b5163; font-weight:700; }
  /* The x is on the slot, so removing one is one click from where you
     can see it rather than scrolling back to the card. */
  .slot .remove { position:absolute; top:-6px; right:-6px; width:18px; height:18px;
                  border-radius:50%; background:#f87171; color:#12141c; border:none;
                  font-size:12px; line-height:1; cursor:pointer; display:none; padding:0; }
  .slot.filled .remove { display:block; }
  #ready { font:inherit; font-weight:600; color:#12141c; background:#a78bfa;
           border:none; border-radius:8px; padding:8px 16px; cursor:pointer;
           transition:opacity .2s, transform .1s; }
  #ready:disabled { opacity:.35; cursor:not-allowed; }
  #ready:not(:disabled):hover { transform:translateY(-1px); }

  /* A card on the team is visibly on it, from the grid. */
  .card { cursor:pointer; transition:outline-color .15s, transform .1s; outline:2px solid transparent; }
  .card:hover { transform:translateY(-2px); }
  .card.picked { outline-color:#6d5ae0; }
  .card.picked::after { content:'on your team'; position:absolute; top:8px; right:8px;
                        background:#6d5ae0; color:#fff; font-size:10px; font-weight:700;
                        padding:2px 6px; border-radius:999px; }
  .card { position:relative; }
  /* Sticky under the topbar, not scrolling away with the grid: picking
     a third fire type otherwise means scrolling back up to find the
     filter you were using. top is the topbar's height. */
  /* top is MEASURED, not guessed.
     .
     It used to be a hardcoded 86px, which is roughly the topbar's height
     on a desktop window and wrong everywhere else: the topbar wraps
     (title, search, three team slots, a button), so on a phone it is two
     or three rows tall and the filters pinned underneath it - visible
     while scrolling, then sliding behind the header and out of reach.
     A script sets --topbar-h from the real element and updates it on
     resize. */
  .filters { position:sticky; top:var(--topbar-h, 86px); z-index:9; background:var(--bg);
             display:flex; flex-wrap:wrap; gap:8px;
             padding:10px 0 12px; margin-bottom:12px;
             border-bottom:1px solid #262a38; }
  .filters a { padding:5px 11px; border:1px solid #333a4d; border-radius:999px;
               color:var(--fg); text-decoration:none; font-size:13px; }
  .filters a.on { background:#2a2f40; border-color:#5b6c8f; }
  .filters small { color:var(--dim); }
  .grid { display:grid; gap:14px;
          grid-template-columns:repeat(auto-fill,minmax(210px,1fr)); }
  .card { background:var(--card); border:1px solid #262b3a; border-radius:12px;
          padding:14px; text-align:center; }
  .card header { display:flex; align-items:baseline; justify-content:center; gap:7px; }
  .num { color:var(--dim); font-size:12px; font-variant-numeric:tabular-nums; }
  .card h2 { margin:0; font-size:16px; text-transform:capitalize; }
  /* The sprites are transparent PNGs with dark outlines, which all but
     vanish against a dark card. A light disc behind them restores the
     contrast they were drawn for. */
  .card img { image-rendering:pixelated; display:block; margin:2px auto;
              background:radial-gradient(circle at 50% 45%, #f2f4f8 58%, transparent 62%);
              border-radius:50%; }
  .types { display:flex; gap:5px; justify-content:center; margin-bottom:10px; }
  .type { padding:2px 9px; border-radius:999px; font-size:11px;
          text-transform:uppercase; letter-spacing:.04em; color:#fff; }
  dl { display:grid; grid-template-columns:auto auto; gap:1px 10px;
       justify-content:center; margin:0 0 10px; font-size:12px; }
  dt { color:var(--dim); } dd { margin:0; font-variant-numeric:tabular-nums; }
  .moves { list-style:none; margin:0; padding:10px 0 0; border-top:1px solid #262b3a;
           font-size:11px; }
  .moves li { display:grid; grid-template-columns:1fr auto auto; gap:8px;
              text-align:left; padding:1px 0; }
  .moves b { font-variant-numeric:tabular-nums; color:var(--dim); min-width:22px;
             text-align:right; }
  .empty { color:var(--dim); padding:32px 0; }
</style>
</head><body>
  <!-- One row: the title, the three team slots, and the button. The
       slots ARE the readout - a sprite says which Pokemon far faster
       than its name does - so there is no text hint beside them. -->
  <header class="topbar">
    <div class="title">
      <h1>Pokedex</h1>
      <!-- The lobby had no link in. "Ready to battle" below OPENS a
           battle, so without this you could create one and never see
           or join anyone else's - the lobby was reachable only by
           typing /battle. Both battle pages already link back here. -->
      <p class="sub">${pokemon.length} pokemon &middot; served from a generated client
        &middot; <a class="battle-link" href="/battle">Battle lobby &rarr;</a></p>
    </div>

    <!-- Filters narrow by type; this narrows by name, which is faster
         when you already know who you want out of a hundred. Client
         side, because every card is already on the page. -->
    <input id="search" type="search" placeholder="Search pokemon..." autocomplete="off">

    <div class="team-slots" id="team">
      <div class="slot" data-slot="0" title="random"><span class="slot-empty">&#127922;</span></div>
      <div class="slot" data-slot="1" title="random"><span class="slot-empty">&#127922;</span></div>
      <div class="slot" data-slot="2" title="random"><span class="slot-empty">&#127922;</span></div>
    </div>

    <button id="ready">Ready to battle</button>
  </header>
  <nav class="filters">${filters}</nav>

  <!-- Asked for only when Ready is pressed without a trainer. Bouncing
       to the lobby instead loses the team you just picked, which is the
       work. -->
  <dialog id="name-modal">
    <form method="dialog" id="name-form">
      <h2>Pick a trainer name</h2>
      <p>Other trainers see this in the lobby.</p>
      <input id="trainer-name" name="name" maxlength="32" placeholder="e.g. Ash" autocomplete="off" required>
      <p class="modal-error" id="name-error" hidden></p>
      <div class="modal-actions">
        <button type="button" id="name-cancel" class="ghost">Cancel</button>
        <button type="submit" id="name-go">Start battling</button>
      </div>
    </form>
  </dialog>
  ${pokemon.length
      ? `<div class="grid">${pokemon.map(card).join('')}</div>`
      : `<p class="empty">No pokemon of that type.</p>`}

  <footer id="downloads" hidden>
    <h2>Play from your terminal</h2>
    <p class="sub" id="dl-platform"></p>
    <div class="dl-row" id="dl-row"></div>
    <p class="dl-other"><a href="https://github.com/ChristopherScot/pokemon/releases/latest" target="_blank" rel="noopener">All downloads &amp; other platforms</a></p>
  </footer>

<script type="module">
// Pin the filters directly below the topbar, whatever height it is.
//
// The topbar wraps - title, search, three team slots, a button - so on a
// phone it is two or three rows tall rather than the one row a desktop
// window gives it. The filters used to pin at a hardcoded 86px, which
// put them BEHIND the header on a narrow screen: they scrolled up, slid
// under the topbar, and could not be reached again without scrolling
// back to the top.
//
// ResizeObserver rather than a resize listener: the topbar also changes
// height when a team slot fills and the text inside it reflows, which
// fires no resize event.
const topbar = document.querySelector('.topbar')
if (topbar) {
  const pin = () => document.documentElement.style.setProperty(
    '--topbar-h', topbar.getBoundingClientRect().height + 'px')
  pin()
  new ResizeObserver(pin).observe(topbar)
}

// The team picker. Click a card to add it, click again or use the x to
// remove it, and "Ready to battle" opens a battle with those three.
//
// The team survives a filter change and a reload, because choosing three
// Pokemon out of a hundred means scrolling and filtering, and losing the
// selection to a click on "fire" would be infuriating.
const KEY = 'pokedex.team'

/** @type {{name: string, sprite: string}[]} */
let team = []
try {
  team = JSON.parse(sessionStorage.getItem(KEY) || '[]').slice(0, 3)
} catch { team = [] }

const slots = [...document.querySelectorAll('.slot')]
const ready = document.getElementById('ready')

function save() {
  try { sessionStorage.setItem(KEY, JSON.stringify(team)) } catch {}
}

function render() {
  slots.forEach((slot, i) => {
    const pick = team[i]
    if (pick) {
      slot.classList.add('filled')
      slot.innerHTML =
        '<img src="' + pick.sprite + '" alt="' + pick.name + '">' +
        '<button class="remove" data-remove="' + i + '" title="remove">x</button>'
    } else {
      slot.classList.remove('filled')
      // A die, not a slot number: an empty slot is filled randomly by
      // the server, and saying so here is what makes "just start a
      // battle" a visible option rather than a hidden one.
      slot.title = 'random'
      slot.innerHTML = '<span class="slot-empty">\u{1F3B2}</span>'
    }
  })

  const names = new Set(team.map((t) => t.name))
  for (const card of document.querySelectorAll('.card')) {
    card.classList.toggle('picked', names.has(card.dataset.name))
  }

  // Never disabled: an empty team is a valid choice, and the server
  // fills what you did not pick. The slots show which is which, so
  // there is nothing to say in words.
  ready.disabled = false

  for (const btn of document.querySelectorAll('[data-remove]')) {
    btn.addEventListener('click', (e) => {
      e.stopPropagation()
      team.splice(Number(btn.dataset.remove), 1)
      save(); render()
    })
  }
}

// Search filters the cards already on the page. No request, because all
// 100 are rendered server-side and a round trip to hide 90 of them would
// be slower than the typing.
const search = document.getElementById('search')
search.addEventListener('input', () => {
  const q = search.value.trim().toLowerCase()
  for (const card of document.querySelectorAll('.card')) {
    // Name and types, so "fire" finds charizard without losing the type
    // filter's count badges.
    const hay = card.dataset.name + ' ' + card.querySelector('.types').textContent.toLowerCase()
    card.classList.toggle('hidden', q !== '' && !hay.includes(q))
  }
})

// Escape clears it, which is what a search field is expected to do and
// saves selecting the text to delete it.
search.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    search.value = ''
    search.dispatchEvent(new Event('input'))
  }
})

document.addEventListener('click', (e) => {
  const card = e.target.closest('.card')
  if (!card) return
  const name = card.dataset.name
  const at = team.findIndex((t) => t.name === name)
  if (at >= 0) team.splice(at, 1)
  else if (team.length < 3) team.push({ name, sprite: card.dataset.sprite })
  save(); render()
})

const modal = document.getElementById('name-modal')
const nameInput = document.getElementById('trainer-name')
const nameError = document.getElementById('name-error')

function showError(msg) {
  nameError.textContent = msg
  nameError.hidden = false
}

/** Opens a battle with the current team. Returns false if it needs a name. */
async function openBattle() {
  const res = await fetch('/battle/open', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    // Only what was actually picked. A short or absent team is the
    // server's cue to fill the rest at random.
    body: JSON.stringify(team.length ? { team: team.map((t) => t.name) } : {}),
  })
  const body = await res.json().catch(() => ({}))
  if (res.ok) {
    sessionStorage.removeItem(KEY)
    location.href = '/battle/' + body.id
    return true
  }
  // 401 is the only recoverable one: it means there is no trainer yet.
  if (res.status === 401) return false
  throw new Error(body.message || 'could not open that battle')
}

ready.addEventListener('click', async () => {
  ready.disabled = true
  ready.textContent = 'opening...'
  try {
    if (await openBattle()) return
    // Ask for the name in place rather than redirecting to the lobby,
    // which would throw away the team just picked - the actual work.
    nameError.hidden = true
    modal.showModal()
    nameInput.focus()
  } catch (err) {
    alert(err.message)
  } finally {
    ready.disabled = false
    ready.textContent = 'Ready to battle'
  }
})

document.getElementById('name-cancel').addEventListener('click', () => modal.close())

document.getElementById('name-form').addEventListener('submit', async (e) => {
  // Not the dialog's default close: the name has to be registered
  // first, and the server can reject it as taken.
  e.preventDefault()
  const name = nameInput.value.trim()
  if (!name) return showError('pick a name')

  const res = await fetch('/battle/register', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    return showError(body.message || 'that name is taken')
  }

  modal.close()
  // Straight on into the battle they asked for, team intact.
  try {
    await openBattle()
  } catch (err) {
    alert(err.message)
  }
})

render()

// Downloads for the terminal clients, matched to the visitor's machine.
//
// Asked of GitHub from the BROWSER rather than from this server. The
// server cannot reach api.github.com at all - its NetworkPolicy allows
// DNS and the pokedex API and nothing else - and routing it through
// here would mean a proxy endpoint, a cache, and a second thing to go
// stale. The visitor's browser already has internet access.
//
// The whole block stays hidden unless a matching asset actually
// exists. pokedex-cli has a release workflow but has never had its
// VERSION bumped, so it has no assets at all; rendering a link to one
// would be a 404 dressed as a download. Listing what the API reports
// means the CLI appears by itself the day it first releases, with no
// change here.
// The releases LIST, not /latest.
//
// Each tool releases under its own version into one shared tag
// namespace, so no single release ever carries both: the TUI is on
// v0.1.1 and the CLI on v0.1.2. Asking for /latest returns whichever
// tool released most recently and hides the other. Walking the list
// newest-first finds the current release of each independently.
const RELEASES = 'https://api.github.com/repos/ChristopherScot/pokemon/releases?per_page=30'

const TOOLS = [
  {
    prefix: 'pokedex-tui',
    name: 'Pokedex TUI',
    blurb: 'Browse and battle in a full-screen terminal app',
    // Inline SVG rather than an icon font or an image: no extra
    // request, no flash of a missing glyph, and it inherits the text
    // colour. A terminal window.
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="M6 9l3 3-3 3M12 15h5"/></svg>',
  },
  {
    prefix: 'pokedex-cli',
    name: 'Pokedex CLI',
    blurb: 'One-shot lookups and scripting',
    // A chevron prompt.
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6l5 6-5 6M12 18h8"/></svg>',
  },
]

// What the asset names call this machine.
//
// Three signals, because no single one is both accurate and widely
// supported:
//
//   - userAgentData.getHighEntropyValues() reports the architecture
//     honestly, including "arm" on an Apple Silicon Mac. Chromium
//     only, and asynchronous.
//   - The WebGL renderer names the GPU, and an Apple Silicon Mac says
//     "Apple". Works in Safari, where the first does not exist, but
//     returns nothing in a headless browser and can be blocked for
//     fingerprinting.
//   - The userAgent string, which on macOS says "Intel" whatever the
//     machine is - so it is the last resort, not the first.
//
// Getting it wrong offers an amd64 build to an arm64 Mac. That runs
// under Rosetta rather than failing, so the cost of a wrong guess is
// a slower binary, not a broken one - which is why this guesses at
// all instead of making everyone read a list.
async function detectPlatform() {
  const ua = navigator.userAgent
  const data = navigator.userAgentData
  const hint = data?.platform || ''
  const os = /Mac|Darwin/i.test(hint + ua) ? 'darwin'
    : /Linux|X11/i.test(hint + ua) && !/Android/i.test(ua) ? 'linux'
    : ''
  if (!os) return null

  let arch = ''
  try {
    const high = await data?.getHighEntropyValues(['architecture'])
    if (high?.architecture) arch = high.architecture === 'arm' ? 'arm64' : 'amd64'
  } catch {
    // Not Chromium, or the call was refused. Fall through.
  }
  if (!arch) {
    try {
      const gl = document.createElement('canvas').getContext('webgl')
      const dbg = gl?.getExtension('WEBGL_debug_renderer_info')
      const renderer = dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : ''
      if (/Apple [GM]/.test(renderer)) arch = 'arm64'
    } catch {
      // Canvas blocked. Fall through.
    }
  }
  if (!arch) arch = /aarch64|arm64/i.test(ua) ? 'arm64' : 'amd64'

  return {
    os,
    arch,
    label: (os === 'darwin' ? 'macOS' : 'Linux') + ' \u00b7 ' +
      (arch === 'arm64' ? (os === 'darwin' ? 'Apple Silicon' : 'ARM64') : 'Intel / AMD'),
  }
}

async function showDownloads() {
  const plat = await detectPlatform()
  if (!plat) return

  let releases
  try {
    const res = await fetch(RELEASES, { headers: { Accept: 'application/vnd.github+json' } })
    if (!res.ok) return
    releases = await res.json()
  } catch {
    // Offline, rate limited, or blocked. The section stays hidden
    // rather than showing an error nobody can act on.
    return
  }
  if (!Array.isArray(releases)) return

  // Newest first, so the first release carrying a tool's asset is its
  // current one. A draft or prerelease is skipped rather than offered.
  const want = '_' + plat.os + '_' + plat.arch + '.tar.gz'
  const links = []
  for (const tool of TOOLS) {
    for (const release of releases) {
      if (release.draft || release.prerelease) continue
      const asset = (release.assets || []).find((a) => a.name === tool.prefix + want)
      if (asset) {
        links.push({ tool, asset, tag: release.tag_name })
        break
      }
    }
  }
  if (!links.length) return

  // Each tool carries its own version, because they are not released
  // together - one line naming a single version would be wrong for
  // whichever tool is not on it.
  document.getElementById('dl-platform').textContent = 'for ' + plat.label
  document.getElementById('dl-row').innerHTML = links.map(({ tool, asset, tag }) =>
    '<a class="dl" href="' + asset.browser_download_url + '" download>' +
    tool.icon +
    '<span><span class="dl-name">' + tool.name + '</span><br>' +
    '<span class="dl-meta">' + tool.blurb + ' \u00b7 ' + tag + '</span></span></a>').join('')
  document.getElementById('downloads').hidden = false
}

showDownloads()
</script>
</body></html>`
}

// register mounts the UI. Kept out of server.js so the scaffold's health,
// metrics and logging setup stays recognisable as the template's.
export function register(app: FastifyInstance) {
  // The type filter arrives as a query parameter; typing it here is
  // what makes request.query.type a string rather than unknown.
  app.get<{ Querystring: { type?: string } }>('/', async (request, reply) => {
    const active = typeof request.query.type === 'string' ? request.query.type : ''

    // Both calls go through the generated client. A failure here is a 502
    // rather than a stack trace: the UI is down because its API is, and
    // saying so is more useful than an empty page.
    //
    // Two different failures have to be caught, which is easy to get
    // wrong. openapi-fetch RETURNS { error } for an HTTP error response -
    // a 404 or a 500 from the API - but a transport failure, where there
    // is no response at all, REJECTS. Checking only `.error` handles the
    // first and lets the second become an unhandled 500 with "fetch
    // failed" as the visitor-facing message.
    let list, typeList
    try {
      ;[list, typeList] = await Promise.all([
        api.GET('/pokemon', { params: { query: active ? { type: active } : {} } }),
        api.GET('/types', {}),
      ])
    } catch (err) {
      request.log.error({ err }, 'pokedex unreachable')
      return reply.code(502).type('text/plain').send('pokedex API unavailable\n')
    }

    if (list.error || typeList.error) {
      request.log.error({ err: list.error || typeList.error }, 'pokedex lookup failed')
      return reply.code(502).type('text/plain').send('pokedex API unavailable\n')
    }

    return reply.type('text/html').send(page({
      pokemon: list.data.pokemon,
      types: typeList.data.types,
      active,
    }))
  })
}
