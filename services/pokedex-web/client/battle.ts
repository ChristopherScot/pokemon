// The battle page's browser code.
//
// A real module, not a string. This lived inside a template literal in
// battle.ts, where the compiler could not see it: a helper used in one
// <script> and declared in another produced a runtime ReferenceError
// that killed every lobby handler in production, and the tests had to
// regex the <script> tag out of the rendered HTML and eval it to reach
// anything.
//
// What the server knows and the browser needs - the battle id, the
// viewer's name, the type colours - arrives through a JSON data island
// rather than string interpolation, so this file is valid TypeScript on
// its own.

import type { components } from '@christopherscot/pokedex-client'

import { V, TERMINAL, esc, effectBand, checkTurn } from './shared.ts'

// The wire types, from the generated schema - the same ones the server
// checks against. The browser code was untyped for its whole life, so
// nothing verified that what it reads off a battle is what the API
// actually sends.
type Battle = components['schemas']['Battle']
type Side = components['schemas']['Side']
type Mon = components['schemas']['BattlePokemon']
type Ev = components['schemas']['BattleEvent']

type Boot = { id: string; me: string; colours: Record<string, string> }

const boot: Boot = JSON.parse(
  document.getElementById('boot')?.textContent || '{}',
)
const ID = boot.id
const ME = boot.me
const COLOURS = boot.colours

// Remember the battle we are in, so the pokedex can offer a way back.
// Set on arrival rather than only when joining, because landing here
// from a link someone sent is the same situation.
try { sessionStorage.setItem('pokedex.battle', ID) } catch {}

let seen = -1
let picked = { attacker: 0, move: 0, target: 0 }
// The battle as last rendered, so attack() can check the turn against
// the same state the player is looking at.
let shownBattle: Battle | null = null
// -1 means "not rendered yet"; the first render adopts the log's
// length rather than replaying it.
let lastLogLen = -1

// The hp percentage each bar is currently DRAWING, so a re-render can
// start the transition from where the bar was rather than from the
// value it is moving to.
// The page's own elements, typed. render() writes to #board and #log on
// every poll, and the untyped version reached straight through
// getElementById(...).scrollTop - which throws if the element is not
// there. It always is today; nothing said so.
function el<T extends HTMLElement = HTMLElement>(id: string): T | null {
  return document.getElementById(id) as T | null
}

const shownHp = new Map<string, number>()

function hpColour(hp: number, max: number) {
  const f = max > 0 ? hp / max : 0
  return f <= 0.2 ? 'var(--danger)' : f <= 0.5 ? 'var(--warn)' : 'var(--good)'
}

function monEl(p: Mon, side: string, i: number, selectable: boolean, selected: boolean) {
  const pct = p.maxHp > 0 ? Math.max(0, p.hp / p.maxHp * 100) : 0
  const types = p.types.map((t: string) =>
    '<span class="type" style="background:' + (COLOURS[t] || '#666') + '">' + esc(t) + '</span>').join('')
  return '<div class="mon' + (p.fainted ? ' fainted' : '') + '"' +
    ' data-slot="' + side + '-' + i + '"' +
    (selectable ? ' data-pick="' + side + '" data-index="' + i + '"' : '') +
    (selected ? ' style="outline:2px solid #6d5ae0"' : '') + '>' +
    '<img src="' + esc(p.sprite) + '" alt="" loading="lazy">' +
    '<div><div class="name">' + esc(p.name) + '</div>' +
    '<div class="types">' + types + '</div>' +
    '<div class="hpwrap"><div class="hp" data-pct="' + pct + '" style="width:' + pct + '%;background:' + hpColour(p.hp, p.maxHp) + '"></div></div></div>' +
    '<div class="hpnum">' + p.hp + '/' + p.maxHp + '</div></div>'
}

// renderSpectator draws a battle this trainer is not in.
//
// A waiting battle gets a join button, because that is the whole point
// of sharing the link. One already underway is read-only - the server
// would reject a third side anyway, and a button that always errors is
// worse than no button.
export function renderSpectator(b: Battle) {
  const joinable = b.status === 'waiting' && b.sides.length < 2
  // Same persistent live region as render(), for the same reason.
  const bannerEl = el('banner')
  if (bannerEl) {
    bannerEl.textContent = joinable
      ? 'this battle is waiting for an opponent'
      : 'watching ' + b.sides.map((s: Side) => s.trainer).join(' vs ')
    bannerEl.className = 'banner theirs'
  }
  let html = ''

  for (const side of b.sides) {
    html += '<div class="side"><h2>' + esc(side.trainer) + '</h2>' +
      side.team.map((p: Mon, i: number) => monEl(p, 'them', i, false, false)).join('') +
      '</div>'
  }

  if (joinable) {
    // A team input, not just a button.
    //
    // Joining from here used to POST no body at all, which the server
    // reads as "no preference" and fills randomly - so the only way to
    // pick your own team was to come through the lobby. Landing on a
    // battle URL someone sent you gave you three Pokemon you did not
    // choose, with nothing on screen suggesting you had a say.
    html += '<div class="pick"><div class="moves" style="flex-direction:column;gap:8px">' +
      '<a class="btn" href="/?join=' + encodeURIComponent(ID) + '">pick your team \u2192</a>' +
      '<div class="sub" style="margin:0">or join straight away and take a random team</div>' +
      '<button id="join-battle">join as ' + esc(ME) + ' with a random team</button>' +
      '</div></div>'
  }

  html += '<div class="log" id="log">' + b.log.slice(-8).map(
    (e) => '<div>' + esc(e.text) + '</div>').join('') + '</div>'

  const board = el('board')
  if (!board) return
  board.innerHTML = html
  board.className = 'board'

  const join = el<HTMLButtonElement>('join-battle')
  if (join) {
    join.onclick = async () => {
      join.disabled = true
      // Empty body: the server fills a random team. Choosing one is
      // the link above, which goes to the pokedex picker.
      const res = await fetch(location.pathname + '/join', {
        method: 'POST',
        headers: { 'content-type': 'application/json', ...V },
        body: JSON.stringify({}),
      })
      if (res.ok) { location.reload(); return }
      // 401 means this trainer no longer exists - the server has already
      // cleared the cookie, so reloading lands on the name prompt rather
      // than leaving a dead button. Reachable whenever the server has
      // restarted since the cookie was set, which is every deploy.
      if (res.status === 401) { location.reload(); return }
      join.disabled = false
      join.textContent = 'could not join - try the lobby'
    }
  }
}

export function render(b: Battle) {
  shownBattle = b
  const mineIdx = b.sides.findIndex((s: Side) => s.trainer === ME)

  // Not a participant: show the battle and a way in, rather than
  // indexing our way into undefined.
  //
  // findIndex returns -1, which made theirsIdx 2, which made BOTH sides
  // undefined, and render threw on the first .team - leaving the board
  // on its initial "loading…" forever with no error anywhere the player
  // could see. Reachable without doing anything strange: open a battle
  // link someone sent you, or keep a trainer cookie across a deploy
  // that cleared the server's in-memory battles.
  if (mineIdx === -1) {
    renderSpectator(b)
    return
  }

  const theirsIdx = 1 - mineIdx
  const mine = b.sides[mineIdx], theirs = b.sides[theirsIdx]
  const myTurn = b.status === 'active' && b.turn === ME

  let banner, cls
  if (b.status === 'finished') {
    banner = b.winner === ME ? '★ you win!' : (b.winner ? b.winner + ' wins' : 'a draw')
    cls = 'over'
  } else if (b.status === 'waiting') {
    banner = 'waiting for an opponent — share the battle id'
    cls = 'theirs'
  } else {
    banner = myTurn ? 'your turn' : 'waiting on ' + esc(b.turn) + '…'
    cls = myTurn ? 'mine' : 'theirs'
  }

  // Written to the persistent node rather than concatenated into html,
  // so the live region survives the innerHTML swap below.
  const bannerEl = el('banner')
  if (bannerEl) {
    bannerEl.textContent = banner
    bannerEl.className = 'banner ' + cls
  }
  let html = ''

  if (theirs) {
    html += '<div class="side"><h2>' + esc(theirs.trainer) + '</h2>' +
      theirs.team.map((p: Mon, i: number) => monEl(p, 'them', i, myTurn && !p.fainted, myTurn && picked.target === i)).join('') +
      '</div>'
  }
  html += '<div class="side"><h2>you</h2>' +
    mine.team.map((p: Mon, i: number) => monEl(p, 'me', i, myTurn && !p.fainted, myTurn && picked.attacker === i)).join('') +
    '</div>'

  if (myTurn) {
    const att = mine.team[picked.attacker]
    html += '<div class="pick"><strong>' + esc(att.name) + '</strong> uses…' +
      '<div class="moves">' +
      att.moves.map((m: Mon['moves'][number], i: number) => {
        // disabled rather than merely styled: the spec says selecting a
        // disabled move is a 409, and this page was not reading the
        // field at all - the move looked identical to every other one.
        const off = att.disabledMove === i
        return '<button data-move="' + i + '"' +
          (off ? ' disabled title="disabled this turn"' : '') +
          (picked.move === i ? ' class="sel"' : '') + '>' +
          esc(m.name) + (m.power ? ' <span style="opacity:.6">' + m.power + '</span>' : ' <span style="opacity:.6">\u2014</span>') +
          '</button>'
      }).join('') +
      '</div><div class="commit">' +
      '<button id="go">attack ' + esc(theirs.team[picked.target].name) + '</button>' +
      '</div></div>'
  }

  html += '<div class="log" id="log">' + b.log.slice(-8).map((e: Ev) => {
    const band = effectBand(e.effectiveness)
    const k = band === 'normal' ? '' : ' class="' + band + '"'
    return '<div' + k + '>' + esc(e.text) + '</div>'
  }).join('') + '</div>'

  const board = el('board')
  if (!board) return
  board.innerHTML = html
  board.className = 'board' + (b.status === 'finished' && b.winner === ME ? ' won' : '')
  const logEl = el('log')
  if (logEl) logEl.scrollTop = 9e9

  // The bars start at the PREVIOUS hp and are moved to the real one on
  // the next frame, so the CSS width transition has something to
  // animate from. Rendering straight to the new value paints the bar
  // already drained - the transition has no start state, and the drop
  // you are meant to watch has already happened.
  for (const [key, was] of shownHp) {
    const bar = board.querySelector<HTMLElement>('[data-slot="' + key + '"] .hp')
    // Number(), not a raw !==: shownHp holds a number and dataset.pct
    // is its string form, so the untyped comparison was ALWAYS true and
    // every bar was rewound every frame rather than only the ones that
    // moved. Invisible because rewinding an unchanged bar to its own
    // value looks like nothing. The compiler found it the moment this
    // stopped being a string.
    if (bar && bar.dataset.pct !== undefined && was !== Number(bar.dataset.pct)) {
      bar.style.width = was + '%'
    }
  }
  requestAnimationFrame(() => {
    for (const bar of board.querySelectorAll<HTMLElement>('.hp')) {
      bar.style.width = bar.dataset.pct + '%'
    }
    for (const [, s] of b.sides.entries()) void s
    shownHp.clear()
    for (const [si, side] of b.sides.entries()) {
      for (const [i, p] of side.team.entries()) {
        const key = (si === mineIdx ? 'me' : 'them') + '-' + i
        shownHp.set(key, p.maxHp > 0 ? Math.max(0, (p.hp / p.maxHp) * 100) : 0)
      }
    }
  })

  // Damage floats come from the NEW log entries, so a poll that brings
  // three turns at once animates all three rather than only the last.
  //
  // lastLogLen starts at the log's length on the FIRST render, not at
  // zero: a battle joined mid-way would otherwise replay every hit that
  // already happened as floats, and then go quiet for the turns that
  // actually arrive while you are watching.
  if (lastLogLen < 0) lastLogLen = b.log.length
  else if (b.log.length > lastLogLen) {
    for (const e of b.log.slice(lastLogLen)) {
      // A plain truthiness test would skip a 0-damage hit, which is
      // exactly the immune case that most deserves a float saying so.
      // Only an event with no damage field at all - a status move or
      // pure narration - has nothing to show here.
      if (e.damage === undefined || e.damage === null || !e.target) continue
      for (const [si, s] of b.sides.entries()) {
        const i = s.team.findIndex((p) => p.name === e.target)
        if (i < 0) continue
        const key = (si === mineIdx ? 'me' : 'them') + '-' + i
        const el = board.querySelector('[data-slot="' + key + '"]')
        if (!el) continue
        const f = document.createElement('div')
        const band = effectBand(e.effectiveness)
        f.className = 'float ' + band
        // An immune hit deals 0, so "-0" says nothing. Name it instead.
        f.textContent = band === 'immune'
          ? 'no effect'
          : '-' + e.damage + (band === 'super' ? ' !!' : '')
        el.appendChild(f)
        // Every hit shakes; a super-effective one shakes harder. Only
        // animating 2x meant most turns had no feedback at all beyond a
        // bar moving.
        if (band !== 'immune') el.classList.add(band === 'super' ? 'hit-hard' : 'hit')
        setTimeout(() => el.classList.remove('hit', 'hit-hard'), 500)
        setTimeout(() => f.remove(), 2400)
      }
    }
    lastLogLen = b.log.length
  }

  // `node`, not `el`: el() is the typed lookup helper above, and
  // shadowing it here read as calling it.
  board.querySelectorAll<HTMLElement>('[data-pick]').forEach((node) => {
    node.addEventListener('click', () => {
      const i = Number(node.dataset.index)
      if (node.dataset.pick === 'me') { picked.attacker = i; picked.move = 0 } else picked.target = i
      render(b)
    })
  })
  board.querySelectorAll<HTMLElement>('[data-move]').forEach((node) => {
    node.addEventListener('click', () => { picked.move = Number(node.dataset.move); render(b) })
  })
  const go = el<HTMLButtonElement>('go')
  if (go) go.addEventListener('click', attack)
}

export async function attack() {
  const go = el<HTMLButtonElement>('go')

  // Refuse what is knowably illegal, naming the reason, rather than
  // spending a round trip to be told "illegal move". The server stays
  // the authority - the 409 path below is untouched.
  const why = shownBattle && checkTurn(shownBattle, ME, picked)
  if (why) {
    const log = document.getElementById('log')
    if (log) {
      const d = document.createElement('div')
      d.textContent = why
      log.appendChild(d)
      log.scrollTop = log.scrollHeight
    }
    return
  }

  if (go) { go.disabled = true; go.textContent = 'attacking…' }
  const res = await fetch('/battle/' + ID + '/turn', {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...V },
    body: JSON.stringify(picked),
  })
  if (!res.ok) {
    // A 409 means the state moved on. Show it rather than silently
    // leaving the button dead.
    const body = await res.json().catch(() => ({ message: 'that move was rejected' }))
    const log = document.getElementById('log')
    if (log) {
      const d = document.createElement('div')
      d.style.color = 'var(--danger)'
      d.textContent = body.message || 'that move was rejected'
      log.appendChild(d)
    }
  }
  poll()

// Auto-start in a browser, stay quiet under a test runner. `document`
// exists in one and not the other, which is the honest distinction -
// this module is for the page.
if (typeof document !== 'undefined' && document.getElementById('board')) {
  start()
}
}

// A battle that is gone stops the loop.
//
// This used to retry forever: a 404 makes res.ok false, the body was
// skipped, and setTimeout was called anyway with no counter and no
// ceiling. Battles live in the server's memory, so a deploy ends every
// one of them - and a tab left open on a finished battle then polled
// once a second for five hours. That traffic is what pushed the web
// service past its memory limit and got it OOMKilled.
//
// Gone is not the same as unreachable: a dropped request or a restart
// mid-rollout should still retry, which is why it takes several
// consecutive misses rather than one.
const MAX_MISSES = 5
let misses = 0


export function stop(message: string) {
  const board = el('board')
  if (board) {
    board.innerHTML = '<div class="banner over">' + message + '</div>' +
      '<div class="pick"><div class="moves">' +
      '<a href="/battle"><button>back to the lobby</button></a>' +
      '</div></div>'
  }
}

export async function poll() {
  try {
    const res = await fetch('/battle/' + ID + '/state', { headers: V })
    if (res.ok) {
      misses = 0
      const b = await res.json()
      if (b.version !== seen) { seen = b.version; render(b) }
    } else if (TERMINAL.has(res.status)) {
      stop('this page is out of date \u2014 reload to carry on')
      return
    } else if (res.status === 404) {
      if (++misses >= MAX_MISSES) {
        const board = el('board')
        if (board) {
          board.innerHTML = '<div class="banner over">this battle is over — ' +
            'the server restarted and battles do not survive it</div>' +
            '<div class="pick"><div class="moves">' +
            '<a href="/battle"><button>back to the lobby</button></a>' +
            '</div></div>'
        }
        return
      }
    }
  } catch { /* a dropped poll is not worth showing; the next one retries */ }
  setTimeout(poll, 1000)
}
// Started by the page, not by the import. A module that polls on load
// cannot be imported by a test without inheriting a timer; the page
// starts it, and a test imports render/poll and drives them directly.
export function start() {
  poll()
}

/**
 * Clears the module's per-page state.
 *
 * The page loads this module once and keeps it for the tab's life, so
 * seen/lastLogLen/shownHp are deliberately module-level - they are what
 * makes "only animate what changed" possible across polls. A test file
 * imports the module once too, which means one test's leftovers are the
 * next one's starting state. This is the seam that makes them
 * independent; nothing on the page calls it.
 */
export function resetForTest() {
  seen = -1
  lastLogLen = -1
  shownBattle = null
  picked = { attacker: 0, move: 0, target: 0 }
  shownHp.clear()
}

// Auto-start in a browser, stay quiet under a test runner. #board only
// exists on the real page.
if (typeof document !== 'undefined' && document.getElementById('board')) {
  start()
}
