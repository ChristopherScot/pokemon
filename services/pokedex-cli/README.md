# pokedex-cli

Owned by me-myself-and-i.

## Usage

Look things up in the pokédex, and play a battle, without leaving the
shell. Everything comes from the `pokedex` API.

```sh
pokedex-cli show pikachu             # one pokemon in detail
pokedex-cli pokemon --type water     # the list, filtered
pokedex-cli pokemon --limit 10
pokedex-cli types                    # every type and how many have it
```

Battling is the same steps the web UI walks you through. Teams are
positional and optional — the server fills in whatever you leave out,
so naming none is the fastest way into a fight:

```sh
pokedex-cli register ash
pokedex-cli open pikachu onix gengar   # or just: pokedex-cli open
pokedex-cli lobby                      # who is waiting
pokedex-cli join <battle-id> charizard blastoise venusaur
pokedex-cli watch <battle-id>          # blocks until it is your turn
pokedex-cli battle <battle-id>         # the current state
```

Attacking is by POSITION, 1-based, exactly as `battle` prints them —
`attack abc123 1 2 3` is your first pokemon using its second move on
their third:

```sh
pokedex-cli attack <battle-id> <my-pokemon> <move> <their-pokemon>
```

A move shown as `(disabled)` cannot be used this turn.

`pokedex-cli update` replaces the binary with the newest release.

It talks to the deployed API by default. Point it at a local one with
`POKEDEX_URL`:

```sh
POKEDEX_URL=http://127.0.0.1:3000 pokedex-cli types
```

Run it without installing: `make run ARGS='show pikachu'`.

## Build

```sh
make build            # compile
make run ARGS='...'   # run without installing
```

Releases are built by CI and published to GitHub Releases;
`pokedex-cli update` pulls the newest one.

## Testing

```sh
make test             # go vet and go test -race. What CI runs.
```

Add a service's own setup to `Makefile.local`. It is not generated, so
it survives a regenerate.

## Deploy

Not deployed — this is a binary people install, not a service. CI
attaches one per platform to a GitHub Release when the version in
`VERSION` changes.
