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
import { colour, island } from './types.ts'
import { assetURL, preloadURLs } from './assets.ts'
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


// escape() runs on every string that reaches the page. The data is ours
// and the names are tame, but a renderer that only escapes "untrusted"
// input is one dataset change away from not escaping at all.
const escape = (s: unknown): string =>
  String(s).replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] ?? c)


// Exported like battlePage/lobbyPage so a test can assert on the real
// markup. The route itself 502s without a live API behind it, so
// testing through inject() cannot see the page at all.
export function page(
  { pokemon, types, active, join = '' }:
  { pokemon: Pokemon[]; types: TypeSummary[]; active: string; join?: string },
) {
  // Every filter link carries the join id along. Dropping it would end
  // the join halfway through: you would filter to "water", lose the
  // battle you were joining, and press a button that opens a new one.
  const keep = join ? `join=${encodeURIComponent(join)}` : ''
  const href = (q: string) => {
    const parts = [q, keep].filter(Boolean).join('&')
    return parts ? `/?${parts}` : '/'
  }
  const filters = [
    `<a href="${href('')}" class="${active ? '' : 'on'}">all</a>`,
    ...types.map((t) =>
      `<a href="${href(`type=${encodeURIComponent(t.name)}`)}" class="${active === t.name ? 'on' : ''}" style="border-color:${colour(t.name)}">${escape(t.name)} <small>${t.count}</small></a>`),
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

  /* Visible to a screen reader, not to the eye. The search-result
     count is announced, and without this rule it was ordinary body
     text sitting under the filter bar reading "5 pokemon match char". */
  .sr-only { position:absolute; width:1px; height:1px; padding:0; margin:-1px;
             overflow:hidden; clip:rect(0 0 0 0); white-space:nowrap; border:0; }

  /* .modal rather than dialog: the trainer-name prompt is a component
     now. Every one of these rules was still scoped to the dialog element, so the
     prompt rendered completely unstyled - no card, no backdrop, no
     input styling - while looking fine in the markup. */
  .modal-backdrop { position:fixed; inset:0; display:grid; place-items:center;
                    background:rgba(9,10,14,.7); backdrop-filter:blur(2px); z-index:50; }
  .modal { border:1px solid #333a4d; border-radius:14px; background:var(--card);
           color:var(--fg); padding:22px 24px; max-width:380px; width:calc(100% - 32px); }
  .modal h2 { margin:0 0 6px; font-size:18px; }
  .modal p { margin:0 0 14px; color:var(--dim); font-size:13px; }
  .modal input { width:100%; font:inherit; color:var(--fg); background:var(--bg);
                 border:1px solid #333a4d; border-radius:8px; padding:9px 12px;
                 outline:none; margin-bottom:14px; }
  .modal input:focus { border-color:#6d5ae0; }
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
  /* 24px, not 18: WCAG 2.5.8 sets 24 as the floor and this is the
     control that undoes a pick. */
  .slot .remove { position:absolute; top:-8px; right:-8px; width:24px; height:24px;
                  border-radius:50%; background:#f87171; color:#12141c; border:none;
                  font-size:12px; line-height:1; cursor:pointer; display:none; padding:0; }
  .slot.filled .remove { display:block; }
  #ready { font:inherit; font-weight:600; color:#12141c; background:#a78bfa;
           border:none; border-radius:8px; padding:8px 16px; cursor:pointer;
           transition:opacity .2s, transform .1s; }
  #ready:disabled { opacity:.35; cursor:not-allowed; }
  #ready:not(:disabled):hover { transform:translateY(-1px); }

  /* A card on the team is visibly on it, from the grid. */
  /* The card is a <button>, so these undo the button defaults it would
     otherwise inherit: centred text, the UA font, a border and padding
     that the grid layout does not want. */
  .card { cursor:pointer; transition:outline-color .15s, transform .1s; outline:2px solid transparent;
          font:inherit; color:inherit; text-align:left; border:0; width:100%; display:block; }
  /* Visible focus. Both text inputs set outline:none and replaced it
     with a border colour at 3.27:1, under the 4.5 it needs - and the
     cards had no focus style at all because they could not be focused. */
  .card:focus-visible, #search:focus-visible, .modal input:focus-visible,
  .filters a:focus-visible, button:focus-visible {
    outline:2px solid #a78bfa; outline-offset:2px;
  }
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
  /* The filters are the primary navigation on a phone; 5px of padding
     put them around 26px tall, under the 44 a finger wants. */
  .filters a { padding:11px 14px; min-height:44px; display:inline-flex; align-items:center; border:1px solid #333a4d; border-radius:999px;
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
  <!-- React mounts here. The pokedex itself is shipped in the boot
       island rather than fetched again by the browser: the server
       already has it, and a second round trip would leave the grid
       empty on first paint. -->
  <div id="root"></div>

<script type="application/json" id="boot">${island({ pokemon, types, active, join })}</script>
${preloadURLs('pokedex').map((u) => `<link rel="modulepreload" href="${u}">`).join('')}
<script type="module" src="${assetURL('pokedex')}"></script>

</body></html>`
}

// register mounts the UI. Kept out of server.js so the scaffold's health,
// metrics and logging setup stays recognisable as the template's.
export function register(app: FastifyInstance) {
  // The type filter arrives as a query parameter; typing it here is
  // what makes request.query.type a string rather than unknown.
  app.get<{ Querystring: { type?: string; join?: string } }>('/', async (request, reply) => {
    const active = typeof request.query.type === 'string' ? request.query.type : ''
    // ?join=<id> means "pick a team, then join THAT battle". The card
    // grid is the only real team picker in the product; without this it
    // was reachable only when opening a battle, so everyone joining one
    // had to type three names from memory into a text box.
    const join = typeof request.query.join === 'string' ? request.query.join : ''

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
      join,
    }))
  })
}
