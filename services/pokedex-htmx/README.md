# pokedex-htmx

Owned by me-myself-and-i.

New here? Start at the [repo README](../../README.md) and get the API
running first; nothing below works without it.

## Usage

The same application as [`pokedex-web`](../pokedex-web) — browse the
pokédex, pick a team of three, open or join a battle, play it out —
with the same screens and the same behaviour. A pixel diff of the
battle board between the two is zero.

The difference is how: the server sends HTML, htmx swaps it in, and
there is no client-side model of the game. It is LAN only, at
`pokedex-htmx.home.chrisscotmartin.com`, because it exists to be read
next to the React version rather than to be a second front door.

### What is worth looking at

**The battle board is zero lines of JavaScript.** It repaints every
second and still animates HP bars, damage numbers and hit-shakes,
because `hx-swap="morph:..."` mutates the existing nodes instead of
replacing them. That only works if everything that animates carries a
stable `id` — see `battle.ts`.

**The server does not render controls you may not use.** A move that is
disabled this turn is absent, not greyed out. A finished battle comes
back with no `hx-trigger`, so polling stops because the control is gone
rather than because a script counted failures. A spectator gets a
different board, not the same one with the buttons switched off. That
deleted an 18-line copy of the server's turn rules that the React
version still has to keep.

**The team is a form, not a session.** Your picks are hidden inputs
posted with the form that submits them, and pushed into the URL — so
they survive a reload, and the link is shareable, which the React
version's `sessionStorage` cannot do.

The JavaScript that remains is deliberate, and `client/page.js` says
why: a search filter over cards already on the page, a `ResizeObserver`
for the sticky header, and a WebGL probe that detects Apple Silicon
because the server cannot. The browser never receives JSON.

## Build

```sh
make deps
POKEDEX_URL=http://127.0.0.1:3000 PORT=3002 INSECURE_COOKIES=1 make run
```

<http://localhost:3002>.

`INSECURE_COOKIES=1` is for local work only. The trainer cookie carries
an API token, so it is `Secure` by default — and a `Secure` cookie is
dropped over plain HTTP, which means without this you can register and
nothing happens.

`make run` uses `npm start`, which passes flags the server needs;
running `node server.ts` directly fails in ways that look like a code
problem. `make run-dist` runs the built bundle from an empty directory,
which is how the image lays it out.

### Layout

| | |
|---|---|
| `routes.ts` | every endpoint |
| `pokedex.ts` | the grid, the team form, the page shell |
| `battle.ts` | the board fragment and its stable ids |
| `lobby.ts` | the lobby and the 3s waiting list |
| `css.ts` | the stylesheets, lifted verbatim from pokedex-web |
| `client/page.js` | the three scripts that remain |
| `vendor/` | htmx and idiomorph, vendored, no build step |

## Testing

```sh
make test         # typecheck, then node --test
```

If you bump htmx or idiomorph in `vendor/`, run `npm run vendor` to
regenerate `vendor/sources.ts`. They are compiled into the bundle as
strings on purpose: the image ships `dist/server.js` and nothing else,
so a file read from disk at runtime works locally and 500s in
production.

## Deploy

CI builds and pushes an image on every push to `main`;
argocd-image-updater rolls it out. Manifests are generated from
`config.yaml` by `homelabctl render` — change the config, not `deploy/`.

There is deliberately no `public: true` host: this service is reachable
on the LAN only.
