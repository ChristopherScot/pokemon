# pokedex-tui

Owned by me-myself-and-i.

## Usage

The whole pokédex and a full battle, full-screen in a terminal.

```sh
pokedex-tui
```

Four screens: browse, lobby, team picker, battle. Every screen prints
its own keys along the bottom, so there is nothing to memorise:

| | |
|---|---|
| `↑` `↓` / `j` `k` | move |
| `enter` | pick — a pokemon, a battle, a move |
| `/` | filter by name |
| `b` | the battle lobby |
| `ctrl+r` | open a battle as your trainer |
| `g` | back to a battle you wandered away from |
| `backspace` | undo the last pick |
| `esc` | back |
| `q` | quit |

It talks to the deployed API by default. Point it at a local one with
`POKEDEX_URL`:

```sh
POKEDEX_URL=http://127.0.0.1:3000 pokedex-tui
```

`pokedex-tui update` replaces the binary with the newest release.

## Build

```sh
make build            # compile
make run ARGS='...'   # run without installing
```

Releases are built by CI and published to GitHub Releases;
`pokedex-tui update` pulls the newest one.

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
