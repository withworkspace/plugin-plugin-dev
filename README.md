# plugin-dev

The ws plugin for writing ws plugins. It starts one from a template, builds it and runs it in your workspace on every save, and shows how each of the workspace's plugins stands.

It's a plugin like any other, written with [the plugin framework](https://github.com/withworkspace/plugin).

## Installing it

```bash
ws plugin add withworkspace/plugin-plugin-dev
```

Run in a workspace. ws downloads this machine's build of the latest release and starts it. It adds `ws plugin-dev`, and *Plugins* to the web UI's sidebar. `ws plugin update` fetches a newer release later.

## Writing a plugin with it

```bash
ws plugin-dev new notes
```

writes `plugin-notes/`: a Go program with an app for ws's web UI, two commands, a setting, a table of its own, a guide for agents and a test. It works as it's written. It builds the new plugin's app (`npm install` and `npm run build`, in its `ui/`) and sets up its Go module, so it needs npm and Go, and the network for both. The app's packages need npm 11 or later, which comes with Node 24.

```bash
cd plugin-notes
```

```bash
ws plugin-dev watch
```

builds it, links it into the workspace and starts it: *Notes* is in the sidebar, and `ws notes say` is a command. Then, on every save, it builds it and starts it again, and its open app loads itself afresh. When the save is of a file of its app (under `ui/`), the app is built first (`npm run build`). When it's of the plugin's Go, the app's types of the plugin's endpoints are written again beside the build (`go tool apigen`), and where they changed the app is built with them. What the plugin logs is shown as it's written. A build that fails is shown, and the last one that built keeps running. Ctrl-C stops watching; the plugin stays linked and running.

| Command | What it does |
|---|---|
| `ws plugin-dev new NAME` | A new plugin in `plugin-NAME/` (`--dir` for somewhere else; `--no-build` and `--no-module` leave its app and its Go module to be set up by hand) |
| `ws plugin-dev watch [FOLDER]` | Build it, and its app when that changed, and run it in the workspace, again on every save. A save of its Go writes its app's types of its endpoints again. |
| `ws plugin-dev migration NAME` | The next numbered file in the plugin's `migrations/` |
| `ws plugin-dev check [FOLDER]` | What it registers, and what's wrong with it |

## A new plugin's app

A plugin's web UI is one client-side app, and ws serves the page it runs in: a plugin has no HTML. The new plugin's is in its `ui/`:

| | |
|---|---|
| `ui/src/app.tsx` | The app: two screens, in [Preact](https://preactjs.com), on [`@withworkspace/ui-lib`](https://www.npmjs.com/package/@withworkspace/ui-lib). It connects to ws, asks the plugin for what it shows, sets its title, shows the screen at the path ws says it's at, and asks again when the plugin says what it shows is stale. |
| `ui/src/ws.ts` | Its hooks onto ws: `usePath` (ws keeps the history, and this is where it says the app is), `useTitle`, and `useAsk` for what a screen asks the plugin. |
| `ui/src/api.gen.ts` | What the plugin's endpoints take and answer, as the app's types: written from `main.go` by `go tool apigen`, and committed. The app asks through `ui-lib`'s `endpoints`, given it, so it can't ask for what isn't there or read what isn't sent. |
| `ui/src/app.css` | Its styles, in ws's tokens (`--ws-*`), so it's in ws's theme from its first paint |
| `ui/src/app.test.ts` | Its test, against `ui-lib`'s stand-in for ws |
| `ui/dist/` | The app as built, which is committed: the plugin is built with what's there, so Go alone builds, tests and installs it |

`main.go` declares the app (`p.UI("ui/dist")`), its entry in the sidebar (`p.View`), and what the app asks the plugin: endpoints, each with the Go types it takes and answers (`plugin.Get(p, "/api/visits", …)` answers a `Visits`). The new plugin's module has the framework's `apigen` as a tool, which writes those types for the app. `watch` runs it; by hand it's `go tool apigen`, and a pull request to the new plugin fails if `ui/src/api.gen.ts` isn't what its Go says ([What the app asks the plugin](https://github.com/withworkspace/plugin#what-the-app-asks-the-plugin)).

`ws plugin-dev check` is `ws plugin check`: it says what the plugin registers, with its app's files (`ui`), and what's wrong with it. A view with no app, an app whose files aren't served as what they are, and a plugin that serves HTML where ws serves its page are each wrong.

A plugin written this way shows only in a ws that serves a plugin's page itself. In an older one, its view is the plugin's 404.

## Publishing a plugin

A new plugin comes with two workflows for GitHub Actions:

- **`release.yml`**: pushing a tag `v<version>` builds the plugin for macOS and Linux, on Intel and ARM, and publishes the builds as a GitHub release with their checksums. That release is what `ws plugin add owner/repo` installs. The tag is the version: `plugin.toml` has none in the repo, and each build's is given the tag's.
- **`pr.yml`**: a pull request is vetted, built and tested, its app is tested, and it fails if `ui/dist/` isn't what `ui/src/` builds.

```bash
git tag v0.1.0
```

```bash
git push origin v0.1.0
```

A plugin with no release is still installable: ws fetches its source and builds it, which needs Go on the machine installing it.

## The Plugins view

Every plugin the workspace has: where it stands, what it adds, and for one being watched, how its last build went. A plugin's own screen has what it registers, its last build's output, the end of its log, and buttons to restart it and to check it.

It's an app like the one a new plugin has (`ui/`), and it asks this plugin for what it shows:

| | |
|---|---|
| `GET /api/plugins` | The workspace's plugins |
| `GET /api/plugins/{name}` | One of them |
| `POST /api/plugins/{name}/restart` | Start it afresh |
| `POST /api/plugins/{name}/check` | Run `ws plugin check` on it |

## How it works

It asks ws for what it needs as anything else does, by running ws's commands:

| It needs | It runs |
|---|---|
| To build a plugin and run what was built | `ws plugin add FOLDER --link --build -f json` |
| The workspace's plugins | `ws plugin list -f json` |
| What's wrong with one | `ws plugin check FOLDER` |
| To start one afresh | `ws plugin restart NAME` |

Everything else is its own: the template (`scaffold/`), noticing a save, and remembering how each build went. A new plugin is given the version of the framework this one was built with, which is the version its template is written for, and its app is given the versions of `@withworkspace/ui-lib` and the rest that this one's own app has (`ui/package.json`).

It can't watch itself: running what it built would stop what's watching. To work on it, build it and run `ws plugin restart plugin-dev`.

## Working on it

```bash
go build -o ws-plugin-dev .
```

```bash
go test ./...
```

The tests run it against a stand-in for ws. One of them builds a new plugin from the template, with its app, and runs that plugin's own tests, which needs npm and the network; `go test -short ./...` leaves it out, and it's skipped where the new plugin can't be set up.

Its app is in `ui/`, and `ui/dist/` is committed. After a change to `ui/src/`:

```bash
just ui
```

builds it (`npm ci` and `npm run build`, which need Node 24), and `ui/dist/` is committed with the change. `just ui-check` fails if what's committed isn't what the source builds, as a pull request does. The app's own test:

```bash
cd ui && npm test
```

## Releasing

A release is a tag `v<version>` on `main`: `release.yml` builds it for each platform, writes the tag's version into each build's `plugin.toml`, and publishes the builds. `plugin.toml` has no version in the repo, so no change sets one. It depends on a released version of the framework (`go.mod`), never a commit of its `main`.

v0.2.0 is the first on the framework's v0.2.0, where a plugin's pages became an app: the plugins it starts have an app, and so does it.

v0.3.1 is on the framework's v0.4.0: its app and a new plugin's are Preact apps, ws keeps their history, and a new plugin starts on v0.4.0.
