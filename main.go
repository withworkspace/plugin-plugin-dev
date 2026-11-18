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
	"time"

	"github.com/withworkspace/plugin"
)

// What's built into the executable: the guide for agents, the app as
// built (ui/dist, from ui/, which is committed so Go alone builds this),
// the template a new plugin's written from, and the app's package.json,
// which says the versions a new plugin's app is given.
//
//go:embed agents.md ui/dist ui/package.json all:scaffold
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
	p.Command("new", "Start a new plugin: an app, commands, a setting and a table, ready to run", d.new).
		Args("NAME").
		Option("dir", "the folder to write it into (default plugin-NAME, where this is run)").
		Flag("no-build", "leave its app to be built by hand (npm install and npm run build, in ui/)").
		Flag("no-module", "leave its Go module to be set up by hand").
		Timeout(10 * time.Minute) // npm install and go get, each from the network

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

	// The app, and its entry in ws's sidebar: every plugin the workspace
	// has and how each stands, and one plugin at /p/<name>. ws serves the
	// page; the app is ui/, and it asks what's below.
	p.UI("ui/dist")
	p.View("Plugins", plugin.Icon("tool"))
	p.HandleFunc("GET /api/plugins", d.apiPlugins)
	p.HandleFunc("GET /api/plugins/{name}", d.apiPlugin)
	p.HandleFunc("POST /api/plugins/{name}/restart", d.apiRestart)
	p.HandleFunc("POST /api/plugins/{name}/check", d.apiCheck)
}
