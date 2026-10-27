# pokedex

Owned by me-myself-and-i.

## Usage

The API, and the only service with a database. It owns the pokédex data
and every rule of a battle — damage, type effectiveness, whose turn it
is — so the five clients (two web front ends, a TUI, a CLI and an
Android app) never re-implement any of it.

```sh
curl localhost:3000/pokemon/pikachu
curl localhost:3000/types
curl localhost:3000/trainers/waiting
```

Battles are `POST /battles`, `POST /battles/{id}/join` and
`POST /battles/{id}/turn`, all authenticated with an
`X-Trainer-Token` from `POST /trainers`. `openapi.yml` is the full
list.

### The spec is the source of truth

`openapi.yml` describes this API. Everything else is generated from it:

| path | what | regenerate with |
|---|---|---|
| `api/oas_*.go` | server interface, types, validation, Go client | `homelabctl regen` |
| `clients/ts/schema.d.ts` | TypeScript types | `homelabctl regen` |

**Change the API in `openapi.yml`, never in Go.** Add a path, run
`homelabctl regen`, and the build will fail until you write the handler:

```
service does not implement api.Handler (missing method GetThing)
```

That is the point — the code cannot drift from the spec, because it will
not compile if it does. CI runs `homelabctl regen` and fails on a diff, so
a spec change cannot merge without the code that matches it.

### Calling this service

```go
c, err := api.New(url, "your-service-name")
```

The second argument is YOUR service's name, not the one being called: it
goes out as `Client-Name`, so whoever operates this service can tell who
is calling.

With no options that gives a 5s timeout, one retry, `Retry-After` obeyed
up to 30s, and no breaker. One retry rather than five, because five can
mean five times the traffic to a dependency that is already struggling.

```go
c, err := api.New(url, "your-service-name",
    api.WithPolicy(api.ExponentialRetry{}),
    api.WithBreaker(5, 30*time.Second),
)
```

### Tracing

`WithTransport` takes any `http.RoundTripper`, which is the seam
OpenTelemetry's instrumentation plugs into:

```go
api.WithTransport(otelhttp.NewTransport(http.DefaultTransport))
```

Retries live above the transport, so a retried request produces one
span per attempt rather than one span with a retry count.

## Build

```sh
docker compose up -d             # postgres, on :15432
make run                         # PORT=3000 by default
wgo run .                        # the same, restarted on every save
homelabctl regen                 # after editing openapi.yml
homelabctl check deploy          # deploy manifests and spec problems
homelabctl diff                  # what would change in the GitOps repo
```

Run it with `DATABASE_URL` set, or you get the in-memory store and the
pokédex compiled into the binary — which is not what production does.
The startup log says which one you got.

### Layout

```
openapi.yml          the API contract
main.go              process lifecycle: logging, timeouts, shutdown
server.go            handlers, metrics middleware, route mounting
api/                 generated, plus two files that are not:
  client.go            timeouts, retries, circuit breaking
  paging.go            cursor iteration as a range loop
clients/ts/          the TypeScript client
deploy/              Kubernetes manifests, rendered from config.yaml
config.yaml          what this service is, for homelabctl
```

`server.go` is the seam: a service whose shape the spec cannot express - a
reverse proxy, say - replaces that one file with a hand-written
`handler()`, and everything else stays as generated. Its header lists what
a replacement has to keep.

`api/client.go` and `api/paging.go` are hand-written and live in `api/` on
purpose: a consumer imports that package, and getting the protocol client
without the defaults would be worse than useless.

### Changing the API and its consumers in one commit

Every consumer in this repo builds against the client in this directory,
not against a published one. So an API change is testable across all four
services before it is committed, and lands as a single commit rather than
a spec PR, a publish, and a follow-up PR per consumer.

Two mechanisms, one per language:

| consumer | wiring | where |
|---|---|---|
| pokedex-cli, pokedex-tui | `replace ... => ../pokedex` | their `go.mod` |
| pokedex-web | `"@christopherscot/pokedex-client": "file:../pokedex/clients/ts"` | its `package.json` |

The loop:

```sh
cd services/pokedex
$EDITOR openapi.yml              # change the contract
homelabctl regen                 # rewrites api/ AND clients/ts/
go test ./...                    # server matches the new spec

cd ../pokedex-cli && go test ./...     # sees it immediately
cd ../pokedex-tui && go test ./...
cd ../pokedex-web && npm test          # also immediately
```

Nothing is published in that loop. `npm publish` happens in CI, only when
`info.version` names a version not already on npm — a deploy that does not
touch the spec publishes nothing.

### Two things that make the web side work

`file:` symlinks the client into `node_modules`, and that has two
consequences worth knowing before you touch either file:

- **The consumer builds the client.** It ships TypeScript and compiles
  to `dist/`, which npm resolves `main` into - but npm does not run a
  `file:` dependency's own lifecycle scripts, so nothing builds it
  unless the consumer does. That is the `client` target in each
  Makefile and the matching step in each Dockerfile. Without it `tsc`
  reports the package as missing entirely, which sends you looking in
  the wrong place.
- **`resolve.preserveSymlinks: true` in both vite configs, and
  `--preserve-symlinks` on the `node` scripts.** Resolution follows the
  symlink to its REAL path, walks up from `services/pokedex/clients/ts/`
  looking for `node_modules`, finds none, and fails. This is true of
  Node and of the bundler alike - Node realpaths by default, which is
  exactly what the flag turns off. Miss the vite side and the container
  build fails; miss the node side and `npm start` and `dev:fast` fail
  while the tests still pass, because vitest resolves through vite.

### Where the transport tests live

`api/` and `clients/ts/` report no test files, and that is deliberate.

The retries, the breaker and the `Retry-After` handling are generated
from one template per language in oapi-clientgen, and its own CI
renders a client and runs the suite against it - 29 Go tests and 32
TypeScript ones, covering the same edges in both. They used to ship
here as `api/client_test.go`, which meant every consumer ran a
byte-identical copy of tests for somebody else's template, and a
failure landed in a file that correctly says "do not edit" in front of
someone who could do nothing about it.

What protects this repo is the regen drift check in CI: it runs
`homelabctl regen` and fails on any diff, so the committed client
cannot drift from the generator that is tested.

### Restart `npm run dev` after regen

`vite build --watch` does not see edits made through the symlink, so a
`homelabctl regen` while the dev server is running leaves it serving the
previous client. Restart it. Editing pokedex-web's own files still
triggers a rebuild normally.

### If the client gains a dependency

It has none. The generated client is self-contained: its transport is
`fetch`, and the resilience around it is plain TypeScript.

That is worth keeping. npm does not install a symlinked package's own
dependencies, so any runtime dependency the client gained would have to
be declared in every consumer as well - and a MISSING one fails loudly
while a SKEWED one does not. Two independent version ranges with
nothing holding them in step is a silent wrong-version bug, which is
why having zero is better than having one well-documented one.

### The consumer's CI has to watch this directory

`file:` makes `services/pokedex/` a build input to pokedex-web, and a
path filter that only lists the consumer's own directory will skip the
job that would have caught a breaking change.

`pokedex-web.yaml` therefore triggers on `services/pokedex/clients/ts/**`
and `services/pokedex/openapi.yml` as well as its own tree. Any new
consumer needs the same.

This matters more than it sounds, because `vite build` does not
typecheck - a renamed field compiles fine and renders as a dash. The
typecheck job is the only thing that catches it, so a filter that skips
that job lets the break ship.

### Versions, and what does not bump

Three separate things, and only the middle one is tied to the spec:

| | when it changes |
|---|---|
| deployed image | every merge to main - image-updater watches the `:latest` digest |
| client version | only when `info.version` changes in `openapi.yml` |
| npm publish | only when that version is not already on npm |

A deploy does not bump a client version. `file:` keeps the consumer on
whatever the spec currently says, so the dependency range never needs
editing - which is what previously let pokedex-web sit on `^0.6.0` while
the spec had moved to 0.7.0.

## Testing

```sh
make test                        # what CI runs
```

### Why a real database matters here

Battle state round-trips through postgres as JSON, so a field the
encoder cannot see is lost there and nowhere else. That is not
hypothetical: `baseStats` had unexported fields, every reloaded
combatant fought with zero attack and defense, and every move in the
game dealt exactly 1 damage. The in-memory store never serialises
anything, so nothing caught it.

The tests that need a database skip without a DSN, **and a skip reads as
a pass**:

```sh
docker compose up -d
POKEDEX_TEST_DSN=postgres://postgres:test@127.0.0.1:15432/pokedex?sslmode=disable \
  go test ./...
```

Each run gets its own schema, named for its process, and drops it on
the way out - so runs can overlap, and none of them touches the data
you have been developing against. It works by pointing `search_path`
at that schema, which only redirects a query that names no schema
itself: write `public.battles` in a migration and the suite goes back
to dropping whatever the DSN points at. `TestMigrationsNameNoSchema`
is what stops that happening quietly.

That includes `TestAWholeGameOverPostgres`, which plays a battle from
registration to a winner. CI runs the same image with the same settings
and fails if that test skips.

The same container also closes the gap between a local run and a
deployed one. `DATABASE_URL` switches BOTH halves together — the pokedex
is loaded from the database instead of the embedded JSON, and battles go
to the postgres store instead of memory — so this is what production
does, not an approximation of it:

```sh
docker compose up -d
DATABASE_URL=postgres://postgres:test@127.0.0.1:15432/pokedex?sslmode=disable go run . seed
DATABASE_URL=postgres://postgres:test@127.0.0.1:15432/pokedex?sslmode=disable go run .
```

Without `DATABASE_URL` the server still starts — embedded pokedex, state
in memory — which is fine for a quick look at an endpoint, and the log
line says which one you got. It is not where you confirm a battle works.

Bump `info.version` in `openapi.yml` when the API changes, then
`homelabctl regen` — it syncs the version the clients report. CI tags the
repo on a version change, and that tag is how both clients are released:

```sh
go get github.com/christopherscot/pokedex@v0.2.0
npm install git+https://github.com/christopherscot/pokedex#v0.2.0
```

## Deploy

Push to main. CI builds the image; argocd-image-updater sees the new digest
and commits it to the homelab repo; ArgoCD syncs it. No homelab credential
lives in this repo.

`deploy/` is rendered from `config.yaml`. Adjust it with `patches` there,
keyed by resource kind, rather than editing the manifests — a patch merges
into what the tool generates, so later convention changes still reach this
service.
