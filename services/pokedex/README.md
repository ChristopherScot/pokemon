# pokedex

Owned by me-myself-and-i.

## The spec is the source of truth

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

## Layout

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

## Working on it

```sh
docker compose up -d             # the database the tests want
go test ./...                    # server, client and paging
go run .                         # PORT=3000 by default
homelabctl regen                 # after editing openapi.yml
homelabctl check deploy          # deploy manifests and spec problems
homelabctl diff                  # what would change in the GitOps repo
```

### Run the tests against a real database

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

## Changing the API and its consumers in one commit

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

- **`openapi-fetch` is declared in pokedex-web too.** npm does not install
  a symlinked package's own dependencies, so the client's import of it
  resolves against pokedex-web's `node_modules`. Removing it there breaks
  the build with "Cannot find package 'openapi-fetch'", pointing at a file
  in a directory that looks unrelated.
- **`resolve.preserveSymlinks: true` in both vite configs, and
  `--preserve-symlinks` on the `node` scripts.** Resolution follows the
  symlink to its REAL path, walks up from `services/pokedex/clients/ts/`
  looking for `node_modules`, finds none, and fails. This is true of
  Node and of the bundler alike - Node realpaths by default, which is
  exactly what the flag turns off. Miss the vite side and the container
  build fails; miss the node side and `npm start` and `dev:fast` fail
  while the tests still pass, because vitest resolves through vite.

### Restart `npm run dev` after regen

`vite build --watch` does not see edits made through the symlink, so a
`homelabctl regen` while the dev server is running leaves it serving the
previous client. Restart it. Editing pokedex-web's own files still
triggers a rebuild normally.

### If the client gains a dependency

The client declares `openapi-fetch` today, and pokedex-web declares it
too so the symlink can resolve it. Add a SECOND dependency to the client
and that one needs the same treatment - declare it in every consumer as
well.

The failure is loud and names the package, so this cannot ship broken:

```
[vite]: Rollup failed to resolve import "ulid" from
  ".../node_modules/@christopherscot/pokedex-client/index.js"
```

Fix by adding it to the consumer's `package.json`. This is rare - the
generated client has had one dependency for its whole life - so the cost
is a build failure with an obvious fix rather than anything structural.

A MISSING dependency fails loudly like that. A SKEWED one does not:
pokedex-web declares `openapi-fetch: ^0.17.0` independently of the
client's own range, and nothing keeps the two in step. Move the client
to `^0.18.0` and npm still resolves pokedex-web's `^0.17.0`, so the
client runs against a major version it did not ask for, with no error.
When you change that range in one place, change it in both.

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

## Calling this service

```go
c, err := api.NewClient(url, api.WithClient(api.NewHTTPClient(api.HTTPOptions{})))
```

The zero value is the intended default: a 5s timeout, one retry, no
breaker. One retry rather than five, because five can mean five times the
traffic to a dependency that is already struggling. Pass
`api.ExponentialRetry{}` or `api.NoRetry{}`, and a `Breaker`, when a
specific call wants something else.

## Deploying

Push to main. CI builds the image; argocd-image-updater sees the new digest
and commits it to the homelab repo; ArgoCD syncs it. No homelab credential
lives in this repo.

`deploy/` is rendered from `config.yaml`. Adjust it with `patches` there,
keyed by resource kind, rather than editing the manifests — a patch merges
into what the tool generates, so later convention changes still reach this
service.
