`ws plugin-dev` is for writing ws plugins:

- `ws plugin-dev new NAME` writes a new plugin into `plugin-NAME/`: a Go program that works as written, with a page, commands, a setting, a table and a test.
- `ws plugin-dev migration NAME`, run in a plugin's folder, writes the next file of its database's migrations.
- `ws plugin-dev check [FOLDER]` builds a plugin, starts it apart from the workspace, and says what it registers and what's wrong with it. Run it before saying a plugin works.
- `ws plugin-dev watch [FOLDER]` builds a plugin and runs it in the workspace again on every save, until it's stopped. It doesn't end by itself: for one build and run, use `ws plugin add FOLDER --link --build`.

A new plugin has a workflow that publishes a release of it when a tag `v<version>` is pushed. Don't push a tag unless you're asked to: it publishes.
