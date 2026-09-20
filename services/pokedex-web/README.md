# pokedex-web

The React front end, and the only service on the public internet
(`pokemon.chrisscotmartin.com`). Everything it shows comes from the
`pokedex` API — it has no database of its own.

New here? Start at the [repo README](../../README.md) and get the API
running first; nothing below works without it.

## Usage

The React front end, and the one the public internet reaches at
`pokemon.chrisscotmartin.com`. Browse the pokédex, pick a team of three,
open or join a battle, and play it out against another trainer.

Everything it shows comes from the `pokedex` API; it has no database.

[`../pokedex-htmx`](../pokedex-htmx) is the same application, feature
for feature, written as server-rendered HTML with almost no JavaScript.
The two render near identically on purpose — a pixel diff of the battle
board between them is zero. Reading them side by side is the point of
having both.

## Build

```sh
make deps
POKEDEX_URL=http://127.0.0.1:3000 PORT=3001 make run
```

<http://localhost:3001>. `PORT` matters: this service and the API both
default to 3000.

Use `npm start`, not `node server.ts` — the script passes
`--preserve-symlinks`, without which Node cannot resolve the generated
API client and the server dies on the first import.

While editing, `npm run dev:fast` restarts on save.

### How it is put together

The server renders the page shell and ships the first payload in a
`<script type="application/json" id="boot">` island; React mounts over
it in the browser. That is why the grid is not in the HTML source but
appears instantly — the data is already on the page, so there is no
second round trip and no empty first paint.

| | |
|---|---|
| `server.ts` | Fastify: routes, logging, metrics |
| `pokedex.ts` | the pokédex page and its CSS |
| `battle.ts` | battle and lobby pages, the battle CSS |
| `client/` | the React components and hooks that run in the browser |
| `client/shared.ts` | the few rules the client needs to know |

The browser talks to *this* server, which talks to the API. It never
calls the pokédex API directly.

## Testing

```sh
make test         # typecheck, then vitest + Testing Library in jsdom
```

It builds the client bundle first, because some tests render against it.

## Deploy

CI builds and pushes an image on every push to `main`;
argocd-image-updater rolls it out. Manifests are generated from
`config.yaml` by `homelabctl render` — change the config, not `deploy/`.

```sh
homelabctl check deploy   # manifests and config
homelabctl status         # what Argo made of it
```
