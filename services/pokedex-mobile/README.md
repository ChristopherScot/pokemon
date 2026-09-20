# pokedex-mobile

Owned by me-myself-and-i.

## Usage

The pokédex and a full battle, as an Android app. Go and
[Gio](https://gioui.org/) — one codebase, no Java or Kotlin.

Five screens: register, browse, lobby, team picker, battle. It talks to
the deployed API, so there is nothing to run alongside it.

On first launch it looks for a saved trainer. On a desktop that is the
same identity file the CLI writes, so a terminal and a window share a
trainer; on Android each app has its own storage, so it asks for a name
instead — which is what the register screen is for.

`make run` opens it in a window on your desktop, which is the fast way
to work on it. `make apk` builds something a phone can install.

## Build

```sh
make run              # on your desktop, in a window
make apk              # an installable Android APK
make apk-check        # is the toolchain here?
```

`make apk` needs a JDK — a modern one. JDK 8 cannot load what gogio
emits; CI pins 17.

The APK it builds is signed with a throwaway debug key, which is enough
for a phone to install after allowing unknown sources. Android
identifies an app by (app id, signing key), so an upgrade over an
install signed with a different key is refused: uninstall first.

## Testing

```sh
make test             # go vet and go test -race. What CI runs.
```

Add a service's own setup to `Makefile.local`. It is not generated, so
it survives a regenerate.

## Deploy

CI builds a signed APK and attaches it to a GitHub Release when the
version in `VERSION` changes. There is no store listing; people install
the APK directly.
