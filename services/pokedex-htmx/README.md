# pokedex-htmx

The same application as [`pokedex-web`](../pokedex-web), built as a
hypermedia app instead of a React one: the server sends HTML, the
browser swaps it in, and there is no client-side model of the game.

It is LAN-only (`pokedex-htmx.home.chrisscotmartin.com`) — it exists to
be compared with the React version, not to be a second front door.

New here? Start at the [repo README](../../README.md) and get the API
running first.

## Run it

```sh
npm install
POKEDEX_URL=http://127.0.0.1:3000 PORT=3002 INSECURE_COOKIES=1 npm start
```

<http://localhost:3002>.

`INSECURE_COOKIES=1` is for local work only. The trainer cookie carries
an API token, so it is `Secure` by default — and a `Secure` cookie is
dropped over plain HTTP, which means without this you can register and
nothing happens.

## What is worth looking at

**The battle board is zero lines of JavaScript.** It repaints every
second and still animates HP bars, damage numbers and hit-shakes,
because `hx-swap="morph:..."` mutates the existing nodes instead of
replacing them. That only works if everything that animates carries a
stable `id` — see `battle.ts`.

**The server does not render controls you may not use.** A move that is
disabled this turn is absent, not greyed out. A finished battle comes
back with no `hx-trigger`, so polling stops because the control is gone
rather than because a script counted failures. That deleted an 18-line
copy of the server's turn rules that the React version has to keep.

**The team is a form, not a session.** Your picks are hidden inputs
posted with the form that submits them, and pushed into the URL — so
they survive a reload, and the link is shareable.

The JavaScript that remains is deliberate and documented in
`client/page.js`: a search filter over cards already on the page, a
`ResizeObserver` for the sticky header, and a WebGL probe that detects
Apple Silicon (the server cannot). The browser never receives JSON.

| | |
|---|---|
| `routes.ts` | every endpoint |
| `pokedex.ts` | the grid, the team form, the page shell |
| `battle.ts` | the board fragment and its stable ids |
| `lobby.ts` | the lobby and the 3s waiting list |
| `css.ts` | the stylesheets, lifted verbatim from pokedex-web |
| `vendor/` | htmx and idiomorph, vendored, no build step |

## Test it

```sh
npm test          # typecheck, then node --test
```

If you bump htmx or idiomorph in `vendor/`, run `npm run vendor` to
regenerate `vendor/sources.ts`. They are compiled into the bundle as
strings on purpose: the image ships `dist/server.js` and nothing else,
so a file read from disk at runtime works locally and 500s in
production.
