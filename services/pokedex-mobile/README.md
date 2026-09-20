# pokedex-mobile

Owned by me-myself-and-i.

## Usage

TODO — what the app does on a phone.

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
