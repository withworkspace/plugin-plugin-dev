# plugin-dev

The ws plugin for writing ws plugins. It starts one from a template, builds it and runs it in your workspace on every save, and shows how each of the workspace's plugins stands.

It's a plugin like any other, written with [workspace-kit](https://github.com/brudil/workspace-kit).

## Installing it

```bash
ws plugin add withworkspace/plugin-plugin-dev
```

Run in a workspace. ws downloads this machine's build of the latest release and starts it. It adds `ws plugin-dev`, and *Plugins* to the web UI's sidebar. `ws plugin update` fetches a newer release later.

## Writing a plugin with it

```bash
ws plugin-dev new notes
```

writes `plugin-notes/`: a Go program with a page, two commands, a setting, a table of its own, a guide for agents and a test. It works as it's written.

```bash
cd plugin-notes
```

```bash
ws plugin-dev watch
```

builds it, links it into the workspace and starts it: *Notes* is in the sidebar, and `ws notes say` is a command. Then, on every save, it builds it and starts it again, and open pages of it load themselves afresh. What the plugin logs is shown as it's written. A build that fails is shown, and the last one that built keeps running. Ctrl-C stops watching; the plugin stays linked and running.

| Command | What it does |
|---|---|
| `ws plugin-dev new NAME` | A new plugin in `plugin-NAME/` (`--dir` for somewhere else) |
| `ws plugin-dev watch [FOLDER]` | Build it and run it in the workspace, again on every save |
| `ws plugin-dev migration NAME` | The next numbered file in the plugin's `migrations/` |
| `ws plugin-dev check [FOLDER]` | What it registers, and what's wrong with it |

## Publishing a plugin

A new plugin comes with two workflows for GitHub Actions:

- **`release.yml`**: pushing a tag `v<version>` builds the plugin for macOS and Linux, on Intel and ARM, and publishes the builds as a GitHub release with their checksums. That release is what `ws plugin add owner/repo` installs. The tag has to match `version` in `plugin.toml`.
- **`pr.yml`**: a pull request is vetted, built and tested.

```bash
git tag v0.1.0
```

```bash
git push origin v0.1.0
```

A plugin with no release is still installable: ws fetches its source and builds it, which needs Go on the machine installing it.

## The Plugins page

Every plugin the workspace has: where it stands, what it adds, and for one being watched, how its last build went. A plugin's own page has what it registers, its last build's output, the end of its log, and buttons to restart it and to check it.

## How it works

It asks ws for what it needs as anything else does, by running ws's commands:

| It needs | It runs |
|---|---|
| To build a plugin and run what was built | `ws plugin add FOLDER --link --build -f json` |
| The workspace's plugins | `ws plugin list -f json` |
| What's wrong with one | `ws plugin check FOLDER` |
| To start one afresh | `ws plugin restart NAME` |

Everything else is its own: the template (`scaffold/`), noticing a save, and remembering how each build went. A new plugin is given the version of workspace-kit this one was built with, which is the version its template is written for.

It can't watch itself: running what it built would stop what's watching. To work on it, build it and run `ws plugin restart plugin-dev`.

## Working on it

```bash
go build -o ws-plugin-dev .
```

```bash
go test ./...
```

The tests run it against a stand-in for ws. One of them builds a new plugin from the template and runs that plugin's own test, which needs the network; `go test -short ./...` leaves it out.
