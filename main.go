// ws-plugin-dev is the ws plugin for writing ws plugins: it starts one
// from a template, builds it and runs it in the workspace on every save,
// and shows how each of the workspace's plugins stands.
//
// It's a plugin like any other. What it needs of ws, it asks for as
// anything else does: by running ws's commands (ws plugin add --link
// --build, ws plugin list, ws plugin check).
package main

import (
	"embed"

	"github.com/withworkspace/plugin"
)

//go:embed agents.md views all:scaffold
var files embed.FS

func main() {
	p := plugin.New(files)
	setup(p)
	p.Run()
}

// setup declares the plugin. It's apart from main so a test can declare
// the same plugin.
func setup(p *plugin.Plugin) {
	p.Describe("Write ws plugins: start one, and run it on every save")

	d := &dev{p: p, watching: map[string]*session{}, checks: map[string]check{}}

	// ws plugin-dev new NAME
	p.Command("new", "Start a new plugin: a page, commands, a setting and a table, ready to run", d.new).
		Args("NAME").
		Option("dir", "the folder to write it into (default plugin-NAME, where this is run)").
		Flag("no-module", "leave its Go module to be set up by hand")

	// ws plugin-dev watch [FOLDER]: runs until it's stopped.
	p.Command("watch", "Build a plugin and run it in the workspace, again on every save", d.watch).
		Args("[FOLDER]").
		Timeout(0)

	// ws plugin-dev migration NAME
	p.Command("migration", "Write the next file of a plugin's database migrations", d.migration).
		Args("NAME").
		Option("dir", "the plugin's folder (default: where this is run)")

	// ws plugin-dev check [FOLDER]
	p.Command("check", "Say what a plugin registers, and what's wrong with it", d.check).
		Args("[FOLDER]")

	// The view: every plugin the workspace has, and how each stands.
	p.View("Plugins", plugin.Icon("tool"), func(v *plugin.View) {
		v.Page("/", "index.html", d.indexPage)
		v.Page("/p/{name}", "plugin.html", d.pluginPage)
		v.Post("/p/{name}/restart", d.restartPost)
		v.Post("/p/{name}/check", d.checkPost)
	})
}
