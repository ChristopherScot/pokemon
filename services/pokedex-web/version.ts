// UI_VERSION is the version of the code that runs IN THE BROWSER, bumped
// by hand when a change to it matters.
//
// Separate from the service's build and from the API version, because
// neither describes what a given tab is running: the script is inlined
// into the page, so a tab holds whatever was current when it loaded and
// keeps running it until something reloads it. A tab loaded before the
// poll fix polled a deleted battle once a second for thirteen hours -
// some 46,000 requests - and no deploy could reach it.
//
// Reported as Client-Version on every request the page makes, so the
// server can see which code a caller is running, and so minVersion in
// config.yaml can refuse one that is doing harm.
//
// Bare, with no leading v: this is what the generated clients send, and
// the server adds the v before comparing.
export const UI_VERSION = '0.3.0'
