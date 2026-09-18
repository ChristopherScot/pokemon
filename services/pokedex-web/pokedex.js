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

import createClient, { exponentialRetry } from '@christopherscot/pokedex-client'

// Where the API lives. The in-cluster address is the default so the
// deployment needs no configuration; $POKEDEX_URL overrides it for a
// port-forward or a local run.
const baseUrl = process.env.POKEDEX_URL || 'http://pokedex.pokedex.svc.cluster.local'

// exponentialRetry rather than noRetry: this is a read-only UI in front
// of a rolling deployment, and a request that lands mid-rollout should
// wait rather than show the visitor an error page.
const api = createClient({ baseUrl, retry: exponentialRetry })

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

const colour = (type) => TYPE_COLOURS[type] || '#6b7280'

// escape() runs on every string that reaches the page. The data is ours
// and the names are tame, but a renderer that only escapes "untrusted"
// input is one dataset change away from not escaping at all.
const escape = (s) =>
  String(s).replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))

function card(p) {
  const types = p.types
    .map((t) => `<span class="type" style="background:${colour(t)}">${escape(t)}</span>`)
    .join('')

  // Status moves have power 0, which reads as a bug rather than as "deals
  // no damage" - show a dash, the same choice the CLI makes.
  const moves = p.moves
    .map((m) => `<li><span>${escape(m.name)}</span><span class="move-type" style="color:${colour(m.type)}">${escape(m.type)}</span><b>${m.power || '—'}</b></li>`)
    .join('')

  return `
    <article class="card">
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

function page({ pokemon, types, active }) {
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
  .filters { display:flex; flex-wrap:wrap; gap:8px; margin-bottom:24px; }
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
  <h1>Pokedex</h1>
  <p class="sub">${pokemon.length} pokemon &middot; served from a generated client</p>
  <nav class="filters">${filters}</nav>
  ${pokemon.length
      ? `<div class="grid">${pokemon.map(card).join('')}</div>`
      : `<p class="empty">No pokemon of that type.</p>`}
</body></html>`
}

// register mounts the UI. Kept out of server.js so the scaffold's health,
// metrics and logging setup stays recognisable as the template's.
export function register(app) {
  app.get('/', async (request, reply) => {
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
