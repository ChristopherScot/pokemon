import test from 'node:test'
import assert from 'node:assert/strict'

import { board, battlePage } from './battle.ts'
import { downloadsFooter, pickLinks } from './downloads.ts'
import { lobbyPage, waitingList } from './lobby.ts'
import { card, filters, grid, page, readyAction, teamSlots, url, type Ctx } from './pokedex.ts'

const move = (name: string, type: string, power: number) => ({
  name, type, power, description: '', effect: '', pp: 15, damageClass: 'special',
})

const mon = (name: string, id = 1) => ({
  id, name, description: '', genus: '', types: ['electric'],
  height: 4, weight: 60, sprite: `/${name}.png`,
  moves: [move('thunderbolt', 'electric', 90)],
})

const ctx = (over: Partial<Ctx> = {}): Ctx => ({
  pokemon: [mon('pikachu', 25), mon('onix', 95), mon('gengar', 94), mon('mew', 151)],
  types: [{ name: 'electric', count: 9 }],
  active: '', join: '', team: [], sprites: {}, resume: '',
  ...over,
})

test('the team is hidden inputs, not a session', () => {
  const html = teamSlots(ctx({ team: ['pikachu'] }))
  assert.match(html, /<input type="hidden" name="team" value="pikachu">/)
})

test('an empty slot offers a random pick', () => {
  const html = teamSlots(ctx())
  assert.equal(html.match(/slot-empty/g)?.length, 3)
})

// A third pick disables every card that is not already on the team, so
// the whole grid changes - which is why /team swaps the grid out of band
// rather than just the toggled card.
test('a full team disables the cards that are not on it', () => {
  const full = ctx({ team: ['pikachu', 'onix', 'gengar'] })
  assert.match(card(mon('mew', 151), full), / disabled/)
  assert.doesNotMatch(card(mon('pikachu', 25), full), / disabled/)
})

test('a picked card says so, for a screen reader too', () => {
  const html = card(mon('pikachu', 25), ctx({ team: ['pikachu'] }))
  assert.match(html, /class="card picked"/)
  assert.match(html, /aria-pressed="true"/)
})

// Dropping either of these turns a join into an open, or loses the picks
// the filter was meant to preserve.
test('a filter link carries the join id and the team', () => {
  const link = url({ active: 'water', join: 'abc', team: ['pikachu', 'onix'] })
  assert.match(link, /type=water/)
  assert.match(link, /join=abc/)
  assert.match(link, /team=pikachu&team=onix/)
})

test('the page posts to join when joining, and to open otherwise', () => {
  assert.match(page(ctx({ join: 'abc' })), /hx-post="\/battle\/abc\/join"/)
  assert.match(page(ctx()), /hx-post="\/battle\/open"/)
  assert.match(page(ctx({ join: 'abc' })), /Join battle/)
  assert.match(page(ctx()), /Ready to battle/)
})

test('a battle in progress offers a way back to it', () => {
  assert.match(page(ctx({ resume: 'xyz' })), /href="\/battle\/xyz"/)
  // Not while you are already looking at that battle's join page.
  assert.doesNotMatch(page(ctx({ resume: 'xyz', join: 'xyz' })), /back to it/)
})

test('the grid carries every pokemon the server returned', () => {
  assert.equal(grid(ctx()).match(/class="card/g)?.length, 4)
})

const battle = (over: Record<string, unknown> = {}) => ({
  id: 'b1', status: 'active', version: 1, turn: 'ash',
  sides: [
    { trainer: 'ash', team: [{ name: 'pikachu', types: ['electric'], hp: 8, maxHp: 10, fainted: false, sprite: '/p.png', moves: [move('thunderbolt', 'electric', 90), move('tackle', 'normal', 40)] }] },
    { trainer: 'misty', team: [{ name: 'staryu', types: ['water'], hp: 10, maxHp: 10, fainted: false, sprite: '/s.png', moves: [move('bubble', 'water', 40)] }] },
  ],
  log: [],
  ...over,
}) as unknown as Parameters<typeof board>[0]['b']

const ZERO = { attacker: 0, move: 0, target: 0 }

// Everything that animates or holds state has to be matchable across a
// morph, or the HP bar renders already-drained and the selection is lost.
test('every element morph must match carries a stable id', () => {
  const html = board({ b: battle(), me: 'ash', sel: ZERO, rejected: '' })
  for (const id of ['id="board"', 'id="banner"', 'id="hp-me-0"', 'id="hp-them-0"',
                    'id="mon-me-0"', 'id="mon-them-0"', 'id="log"']) {
    assert.ok(html.includes(id), `missing ${id}`)
  }
})

test('the board polls with the morph swap, and pauses on a hidden tab', () => {
  const html = board({ b: battle(), me: 'ash', sel: ZERO, rejected: '' })
  assert.match(html, /hx-trigger="every 1s \[!document\.hidden\]"/)
  assert.match(html, /hx-swap="morph:\{ignoreActiveValue:true\}"/)
  assert.match(html, /hx-sync="this:replace"/)
})

// The control is what stops the poll. A finished battle whose fragment
// still carried hx-trigger would poll a settled battle forever.
test('a finished battle stops polling by not rendering the trigger', () => {
  const html = board({ b: battle({ status: 'finished', winner: 'ash' }), me: 'ash', sel: ZERO, rejected: '' })
  assert.doesNotMatch(html, /hx-trigger/)
  assert.match(html, /you win/)
})

// HATEOAS: the server does not render a control you may not use, so
// there is no client-side copy of the turn rules to drift from them.
test('a move disabled this turn is omitted, and the reason stated', () => {
  const b = battle()
  b.sides[0].team[0].disabledMove = 0
  const html = board({ b, me: 'ash', sel: ZERO, rejected: '' })
  assert.match(html, /disabled this turn/)
  assert.doesNotMatch(html, /<input type="radio" name="move" value="0"/)
  // The move that IS usable is still offered.
  assert.match(html, /<input type="radio" name="move" value="1"/)
})

test('it is not your turn, so no controls are rendered at all', () => {
  const html = board({ b: battle({ turn: 'misty' }), me: 'ash', sel: ZERO, rejected: '' })
  assert.doesNotMatch(html, /id="pick"/)
  assert.doesNotMatch(html, /name="move"/)
  assert.match(html, /waiting on misty/)
})

test('an onlooker watches, and is offered no moves', () => {
  const html = board({ b: battle(), me: 'brock', sel: ZERO, rejected: '' })
  assert.match(html, /watching ash vs misty/)
  assert.doesNotMatch(html, /name="move"/)
})

test('an open battle invites an onlooker to pick a team', () => {
  const b = battle({ status: 'waiting', sides: [battle().sides[0]] })
  const html = board({ b, me: 'brock', sel: ZERO, rejected: '' })
  assert.match(html, /\/\?join=b1/)
})

test('the selection the server rendered is the one that is checked', () => {
  const html = board({ b: battle(), me: 'ash', sel: { attacker: 0, move: 1, target: 0 }, rejected: '' })
  assert.match(html, /value="1"[^>]*checked/)
})

// The banner is a live region: replacing the node announces nothing.
test('the banner keeps one identity so a screen reader is told', () => {
  const html = board({ b: battle(), me: 'ash', sel: ZERO, rejected: '' })
  assert.match(html, /id="banner"[^>]*role="status"[^>]*aria-live="polite"/)
})

// The radios live inside the swapped board; the form must not, or a
// half-made choice would be rebuilt every second.
test('the turn form sits outside the polled region', () => {
  const html = battlePage({ id: 'b1', trainer: 'ash', first: '<div id="board"></div>' })
  assert.ok(html.indexOf('id="turn"') < html.indexOf('id="board"'))
})

test('a trainer name cannot break out of the markup', () => {
  const html = battlePage({ id: 'b1', trainer: '<script>alert(1)</script>', first: '' })
  assert.doesNotMatch(html, /<script>alert/)
  assert.match(html, /&lt;script&gt;/)
})

test('the waiting list keys its rows so unchanged ones are left alone', () => {
  const html = waitingList([{ battleId: 'abc', trainer: 'ash', team: ['pikachu'], createdAt: '2026-01-01T00:00:00Z' }])
  assert.match(html, /id="w-abc"/)
  assert.match(html, /hx-trigger="every 3s \[!document\.hidden\]"/)
})

test('the lobby always offers a way to re-register', () => {
  // A deploy invalidates tokens while the cookie survives, so the form
  // has to be reachable even when the page thinks it knows you.
  assert.match(lobbyPage({ me: { name: 'ash', token: 't' }, waiting: [] }), /not you\?/)
  assert.match(lobbyPage({ me: null, waiting: [] }), /id="reg-row"/)
})

test('downloads pick the newest release carrying an asset for this platform', () => {
  const releases = [
    { tag_name: 'v9', draft: true, assets: [{ name: 'pokedex-tui_darwin_arm64.tar.gz', browser_download_url: 'https://gh/draft' }] },
    { tag_name: 'v2', assets: [{ name: 'pokedex-tui_darwin_arm64.tar.gz', browser_download_url: 'https://gh/u2' }] },
    { tag_name: 'v1', assets: [{ name: 'pokedex-cli_darwin_arm64.tar.gz', browser_download_url: 'https://gh/u1' }] },
  ]
  const links = pickLinks(releases, 'darwin', 'arm64')
  assert.deepEqual(links.map((l) => l.url), ['https://gh/u2', 'https://gh/u1'])
  // Each tool carries its own tag, because they are not released together.
  assert.deepEqual(links.map((l) => l.tag), ['v2', 'v1'])
})

test('an unknown platform gets no footer rather than a wrong one', async () => {
  assert.equal(await downloadsFooter('windows', 'amd64'), '')
  assert.equal(await downloadsFooter('darwin', 'sparc'), '')
})

test('github being unreachable hides the footer instead of failing the page', async () => {
  const boom = (() => Promise.reject(new Error('offline'))) as unknown as typeof fetch
  assert.equal(await downloadsFooter('darwin', 'arm64', boom), '')
})

test('the release list is fetched by the server, and HTML comes back', async () => {
  const stub = (async () => new Response(JSON.stringify([
    { tag_name: 'v3', assets: [{ name: 'pokedex-tui_linux_amd64.tar.gz', browser_download_url: 'https://example/t' }] },
  ]), { status: 200 })) as unknown as typeof fetch
  const html = await downloadsFooter('linux', 'amd64', stub)
  assert.match(html, /id="downloads"/)
  assert.match(html, /https:\/\/example\/t/)
  assert.match(html, /Linux · Intel \/ AMD/)
})

test('a download url that is not https never reaches an href', () => {
  const bad = [{ tag_name: 'v1', assets: [
    { name: 'pokedex-tui_darwin_arm64.tar.gz', browser_download_url: 'javascript:alert(1)' },
  ] }]
  assert.deepEqual(pickLinks(bad, 'darwin', 'arm64'), [])
  const good = [{ tag_name: 'v1', assets: [
    { name: 'pokedex-tui_darwin_arm64.tar.gz', browser_download_url: 'https://example/t' },
  ] }]
  assert.equal(pickLinks(good, 'darwin', 'arm64').length, 1)
})

// Every page load fires this once, so a GitHub blackhole must not park
// the handler until the OS gives up.
test('a github fetch that never answers gives up rather than hanging', async () => {
  const started = Date.now()
  const slow = ((_u: string, opts: { signal?: AbortSignal }) =>
    new Promise<Response>((_resolve, reject) => {
      opts?.signal?.addEventListener('abort', () =>
        reject(new Error('aborted')), { once: true })
    })) as unknown as typeof fetch
  assert.equal(await downloadsFooter('darwin', 'arm64', slow), '')
  assert.ok(Date.now() - started < 10_000, 'gave up inside its deadline')
})

// Pick pikachu, then filter to water: the grid no longer contains
// pikachu, so the slot has a name and nowhere to read a sprite from.
// pokedex-web kept the sprite beside the name in sessionStorage.
test('a picked pokemon keeps its sprite after filtering it out of the grid', () => {
  const filtered = ctx({
    pokemon: [mon('squirtle', 7)],
    active: 'water',
    team: ['pikachu'],
    sprites: { pikachu: '/pikachu.png' },
  })
  assert.match(teamSlots(filtered), /src="\/pikachu\.png"/)
  assert.doesNotMatch(teamSlots(filtered), /src=""/)
})

// A pick swaps the slots and the grid; the filter nav has to come with
// them, because every filter link carries the team and a stale one
// silently drops the picks on the next click.
test('the filter links are re-rendered with the team after a pick', () => {
  const picked = ctx({ team: ['pikachu'], types: [{ name: 'water', count: 18 }] })
  const nav = filters(picked, true)
  assert.match(nav, /hx-swap-oob="true"/)
  assert.match(nav, /href="\/\?type=water&team=pikachu"/)
  // and the grid names itself for the same swap
  assert.match(grid(picked, true), /id="grid" hx-swap-oob="true"/)
  assert.doesNotMatch(grid(picked), /hx-swap-oob/)
})

// Ready was a native form submit (formaction), so the browser navigated
// to /battle/open and rendered the name dialog AS THE WHOLE DOCUMENT -
// HX-Redirect means nothing to a native submit, so registering dumped
// you on the lobby with no team and no trainer. It has to be an htmx
// request.
test('committing a team is an htmx request, not a native submit', () => {
  const html = page(ctx({ team: ['pikachu'] }))
  assert.match(html, /<form id="team-form" hx-post="\/battle\/open"/)
  assert.match(html, /hx-target="#dialog"/)
  // and a slot for the dialog to land in
  assert.match(html, /id="dialog"/)
  // no formaction anywhere: that is what made it navigate
  assert.doesNotMatch(html, /formaction=/)
  assert.equal(readyAction(ctx({ join: 'abc' })), '/battle/abc/join')
  assert.equal(readyAction(ctx()), '/battle/open')
})

// The poll morphs the board every second. Without an id on each wrapper
// morph has nothing to match, rebuilds the subtree and DETACHES the
// attack button - a player who clicked as a poll landed lost the click
// and their turn. Playwright reported "element was detached from the
// DOM".
test('the move picker keeps ids so a poll cannot detach the attack button', () => {
  const html = board({ b: battle(), me: 'ash', sel: ZERO, rejected: '' })
  for (const id of ['id="pick"', 'id="moves"', 'id="commit"', 'id="go"', 'id="pick-who"']) {
    assert.ok(html.includes(id), `missing ${id}`)
  }
})
