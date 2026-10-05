# BranchKit Apps

The apps on this machine and the names you call them by: open, switch to, or
start a new window of any app by name. A plugin for
[BranchKit](https://github.com/branchkit), an accessibility plugin platform
for the desktop. MIT licensed.

BranchKit is pre-launch: the app is not publicly released yet.

## What you can say

The phrases come from `commands.json`. Any command can also be bound to a key
in Settings → Keybinds.

| Say | Does |
|---|---|
| `focus <app>` / `switch <app>` (optionally with `to`) | Switch to the app, opening it if it isn't running |
| `new <app>` | Open a new window of the app on this desktop, without switching to the desktop its other windows are on |

`<app>` is any name in the `apps` collection: the app's own name, curated
names ("chrome", "vs code"), and any you add on the Apps tab. Other plugins
capture `<apps>` too — voice's `open app` picker, placement's
`<apps> left` — so a name you add works everywhere.

## Actions

| Action | Does |
|---|---|
| `apps.launch` | Switch to an app, opening it if needed. `app_id` is the app's id or any name it answers to; `new_instance` starts another copy |
| `apps.new_window` | Open a new window of an app on this desktop, opening the app instead when it cannot make one that way |
| `apps.open` | Open a link or file with its default app |

Every action reports failure — an app that will not open, an empty target —
as an error, never as success.

## Collections

- `apps`: one record per name you can say, `{spoken, app_id}`. `app_id` is
  what this OS's platform calls the app: a bundle identifier on macOS, a
  desktop entry on Linux (`google-chrome`), an executable's name on Windows
  (`chrome`). Built from the OS's list of installed apps plus the curated
  names in `core_apps.json`, and rebuilt on wake and whenever an app the
  registry has never seen takes focus, so a newly installed app can be opened
  by name without a restart.
- `app_traits`: what kind of application each app is (`terminal`, `chat`),
  one record per (trait, app) pair. Facts about apps for other plugins to
  interpret. Not a stable surface yet: only this plugin writes it and the
  trait names may change.
- `plugin.apps.config`: settings (below).

Your edits — names added or removed, apps turned off, traits changed — are
user-band overrides on these collections, written through the platform. They
survive every rebuild and are shown back from the collection as it stands,
so what the Apps tab shows is what you can say. Removing a name you added
deletes it; removing one this plugin publishes suppresses it, and adding it
back clears the suppression.

## `core_apps.json`

Curated apps, each with its id on every OS it exists on:

```json
{"name": "Google Chrome", "ids": {"macos": ["com.google.Chrome"], "linux": ["google-chrome"], "windows": ["chrome"]}, "aliases": ["chrome"]}
```

An entry with no id for the running OS does not apply there. An entry with
traits and no aliases is trait-only: it says what kind of app an id is, and
adds no name and no row.

## Settings

- **Apps**: every app with the names you can say for it and its traits; add
  or remove a name or trait, turn an app off so no name opens it.
- **Move the pointer to an app you open** (off by default): after
  `apps.launch`, move the pointer to the centre of the app's window, so
  scrolling lands there.

## Permissions

| Privilege | Why |
|---|---|
| `apps` | List installed and running apps and their windows |
| `apps.control` | Open apps and new windows |
| `filesystem` | `apps.open`, which opens a link or file |
| `input` | Move the pointer, for the setting above |
| `display` | Read the pointer position, for the same |

No network: the manifest declares no hosts, so the sandbox gives it none.

## Reading this as an example

- `src/registry.go` publishes a collection backed by live OS state, and
  rebuilds it on the events that can change it.
- `src/edits.go` is the pattern for letting a person edit what a plugin
  publishes: user-band overrides, restore before add or remove, and every
  screen read back from the composed collection.
- `src/host.go` puts every platform call behind a small interface, so
  `src/fake_test.go` can test the plugin without a running app.

## Build

Go, [plugin-sdk-go](https://github.com/branchkit/plugin-sdk-go). The settings
tab is a [templ](https://templ.guide) template; the generated Go is committed,
so `templ` is needed only when you edit `apps.templ`.

```bash
cd src && go build -o ../apps-plugin . && go test ./...
```

## License

MIT. See [LICENSE](LICENSE).
