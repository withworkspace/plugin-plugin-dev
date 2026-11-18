`ws plugin-dev` is for writing ws plugins:

- `ws plugin-dev new NAME` writes a new plugin into `plugin-NAME/`: a Go program that works as written, with an app for ws's web UI (`ui/`), commands, a setting, a table and a test. It runs `npm install` and `npm run build` for the app, which need npm 11 or later.
- `ws plugin-dev migration NAME`, run in a plugin's folder, writes the next file of its database's migrations.
- `ws plugin-dev check [FOLDER]` builds a plugin, starts it apart from the workspace, and says what it registers and what's wrong with it. Run it before saying a plugin works.
- `ws plugin-dev watch [FOLDER]` builds a plugin and runs it in the workspace again on every save, until it's stopped. It doesn't end by itself: for one build and run, use `ws plugin add FOLDER --link --build`.

A plugin's web UI is an app, in `ui/src/`, built into `ui/dist/`, which is committed. The plugin serves no HTML: ws serves the page. After changing the app, run `npm run build` in `ui/` (`watch` does it on a save) and commit `ui/dist/` with the change.

A new plugin has a workflow that publishes a release of it when a tag `v<version>` is pushed. Don't push a tag unless you're asked to: it publishes.
