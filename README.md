# pokemon

One Pokédex and battle API, and five things that talk to it: two web
front ends, a terminal UI, a CLI, and an Android app.

It exists to be worked on. The same game is built five ways on purpose,
so you can compare them.

```
        pokedex-web ─┐                    React
       pokedex-htmx ─┤                    htmx, server-rendered HTML
        pokedex-tui ─┼──▶  pokedex  ──▶  postgres
        pokedex-cli ─┤     (the API,
     pokedex-mobile ─┘      Go)
```

Everything is a client of `services/pokedex`. It owns the rules, the
data and the battles; nothing else has a database.

---

## 1. Set up your machine

| tool | why | check |
|---|---|---|
| [Go](https://go.dev/dl/) 1.27+ | the API, TUI, CLI, mobile app | `go version` |
| [Node](https://nodejs.org/) 22+ | the two web front ends | `node --version` |
| [Docker](https://docs.docker.com/get-started/get-docker/) | postgres for local runs and tests | `docker ps` |
| [git](https://git-scm.com/downloads) | | `git --version` |

On a Mac, all four in one go:

```sh
brew install go node git
brew install --cask docker   # then open Docker Desktop once
```

Nothing else is required. There is no global CLI to install, no
`make bootstrap`, and you do not need cluster access to run any of this.

## 2. Run it

One command, from the repo root:

```sh
make setup     # checks your tools, installs deps, starts postgres, seeds
make run       # the API and both front ends together
```

`make` is the same entry point in every service, and `make test` is
literally what CI runs. `make` on its own lists what each one offers.

If you would rather do it by hand, or only want one piece:

**Terminal one — the API:**

```sh
cd services/pokedex
docker compose up -d                       # postgres on :15432
export DB='postgres://postgres:test@127.0.0.1:15432/pokedex?sslmode=disable'
DATABASE_URL=$DB go run . seed             # load the 100 pokemon (once)
DATABASE_URL=$DB go run .                  # serves on :3000
```

Wait for `"msg":"started"`. Check it:

```sh
curl -s localhost:3000/pokemon/pikachu | head -c 80
```

**Terminal two — a front end:**

Every service defaults to port 3000, so the front end needs one of its
own:

```sh
cd services/pokedex-web
npm install
POKEDEX_URL=http://127.0.0.1:3000 PORT=3001 npm start
```

Open <http://localhost:3001>. Pick three pokemon, hit **Ready to
battle**, choose a trainer name. To play both sides, open the same URL
in a private window, join from the lobby, and take turns.

Or skip the browser entirely — with the API still running:

```sh
cd services/pokedex-cli
POKEDEX_URL=http://127.0.0.1:3000 go run . show pikachu
POKEDEX_URL=http://127.0.0.1:3000 go run . types

cd ../pokedex-tui
POKEDEX_URL=http://127.0.0.1:3000 go run .
```

### If something does not work

**`address already in use`** — everything defaults to 3000, and it is a
popular port besides. Every service takes `PORT`:

```sh
DATABASE_URL=$DB PORT=19001 go run .
POKEDEX_URL=http://127.0.0.1:19001 PORT=19002 npm start
```

**The CLI or TUI shows data you did not seed** — they default to the
deployed API, not yours. Always pass `POKEDEX_URL` when working locally.

**`no pokemon in the database`** — run the `seed` step above.

**Run the API with `DATABASE_URL`.** Without it the server still starts,
but it keeps battles in memory and reads the pokédex from a JSON file
compiled into the binary. That is not what production does, and the gap
is not cosmetic: a bug once made every move deal exactly 1 damage, and
it could only happen on the database path. The startup log tells you
which one you got — `state is in postgres`, or `state is in memory`.

## 3. Test it

```sh
cd services/pokedex
docker compose up -d
POKEDEX_TEST_DSN=$DB go test ./...      # note: TEST_DSN, not DATABASE_URL
```

Without `POKEDEX_TEST_DSN` the database tests **skip**, and a skip looks
exactly like a pass. One of them plays a whole battle from registration
to a winner — it is the test that would have caught the 1-damage bug.

The node services bring their own runner:

```sh
cd services/pokedex-web  && npm test
cd services/pokedex-htmx && npm test
```

CI runs all of this per service on push, against the same postgres
image.

### What CI does not cover

Each service is tested on its own. Nothing starts the API and a front
end together and drives them, so a break in how they talk to each other
only shows up when someone uses it — which is how two htmx bugs reached
production with every suite green.

`services/pokedex-htmx/e2e/journey.js` is that test, but you have to run
it yourself; it needs a browser and a running stack. The next step is a
compose file that brings every service in the repo up together so CI can
do the same thing, rather than leaving it to whoever remembers.

## 4. The services

Each has its own README with what it is and how to work on it.

| | what it is | port | read |
|---|---|---|---|
| [`services/pokedex`](services/pokedex) | the API and the game rules. Go, generated from `openapi.yml`. | 3000 | [README](services/pokedex/README.md) |
| [`services/pokedex-web`](services/pokedex-web) | the React front end. The one on the public internet. | 3000 | [README](services/pokedex-web/README.md) |
| [`services/pokedex-htmx`](services/pokedex-htmx) | the same app in htmx — HTML over the wire, almost no JavaScript. LAN only. | 3001 | [README](services/pokedex-htmx/README.md) |
| [`services/pokedex-tui`](services/pokedex-tui) | full-screen terminal app. | — | [README](services/pokedex-tui/README.md) |
| [`services/pokedex-cli`](services/pokedex-cli) | one-shot lookups, scriptable. | — | [README](services/pokedex-cli/README.md) |
| [`services/pokedex-mobile`](services/pokedex-mobile) | Android app, Go + Gio. | — | [README](services/pokedex-mobile/README.md) |

Start with [`services/pokedex/README.md`](services/pokedex/README.md):
the spec is the source of truth for every client, and that explains why.

## 5. Changing the API

`openapi.yml` generates the server interfaces **and** the clients, so a
change to both is one commit rather than a publish-and-wait:

```sh
cd services/pokedex
$EDITOR openapi.yml
homelabctl regen          # server stubs + the TypeScript client
```

The web front ends depend on `services/pokedex/clients/ts` by path, so
they pick the change up with no publish step.

## 6. Deploying

You do not need any of this to work on the code. Each service carries
its own `config.yaml` and `deploy/`; CI builds an image per service on
push, `homelabctl` renders the manifests, and Argo CD applies them.
`services/pokedex/README.md` has the detail.
