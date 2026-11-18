package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// What the app (ui/) asks the plugin: every plugin the workspace has, one
// of them, and to restart or check one. Each answers JSON; what goes
// wrong is said in plain text, which the app shows.

// row is one of the workspace's plugins, as the app is told of it.
type row struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	State       string   `json:"state"`
	Error       string   `json:"error,omitempty"`
	Needs       []string `json:"needs"`            // the config it requires that isn't set
	Linked      string   `json:"linked,omitempty"` // the folder of its source, if it's linked to one
	Adds        []string `json:"adds"`             // what it adds, each in a few words
	Watch       *build   `json:"watch,omitempty"`  // how its last build went, if it's being watched
}

// build is how the last build of a plugin being watched went.
type build struct {
	Builds int       `json:"builds"` // how many there have been; 0 while the first is running
	At     time.Time `json:"at"`
	OK     bool      `json:"ok"`
	Output string    `json:"output,omitempty"`
	Error  string    `json:"error,omitempty"`
}

// detail is one plugin: what it registers, how its last check went, and
// the end of its log.
type detail struct {
	row
	Self       bool      `json:"self"` // it's this plugin, which doesn't restart itself
	Views      []string  `json:"views"`
	Commands   []command `json:"commands"`
	Config     []setting `json:"config"`
	GuideLines int       `json:"guide_lines"`
	Check      *check    `json:"check,omitempty"`
	Log        string    `json:"log"`
}

type command struct {
	Usage string `json:"usage"` // ws notes say [NAME]
	Short string `json:"short"`
}

type setting struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Required bool   `json:"required"`
}

func (d *dev) row(s status) row {
	r := row{
		Name: s.Name, Version: s.Version, Description: s.Registration.Description,
		State: s.State, Error: s.Error, Linked: s.Linked,
		Needs: append([]string{}, s.Missing...), Adds: []string{},
	}
	for _, v := range s.Registration.Views {
		r.Adds = append(r.Adds, "a view, "+v.Title)
	}
	if word := s.Word(); word != "" {
		r.Adds = append(r.Adds, "ws "+word)
	}
	if s.GuideLines() > 0 {
		r.Adds = append(r.Adds, "a guide for agents")
	}
	if w := d.watched(s.Name); w != nil {
		r.Watch = &build{Builds: w.Builds, At: w.At, OK: w.OK, Output: w.Output, Error: w.Error}
	}
	return r
}

func (d *dev) detail(s status) (detail, error) {
	log, err := tail(filepath.Join(d.p.Root(), ".ws", "plugins", s.Name, "data", "plugin.log"), 60)
	if err != nil {
		return detail{}, err
	}
	out := detail{
		row: d.row(s), Self: s.Name == d.p.Name(), GuideLines: s.GuideLines(), Log: log,
		Views: []string{}, Commands: []command{}, Config: []setting{},
	}
	for _, v := range s.Registration.Views {
		out.Views = append(out.Views, v.Title)
	}
	if cmds := s.Registration.Commands; cmds != nil {
		for _, c := range cmds.Subcommands {
			out.Commands = append(out.Commands, command{Usage: strings.TrimSpace("ws " + s.Word() + " " + c.Name + " " + c.Args), Short: c.Short})
		}
	}
	for _, k := range s.Registration.Config {
		out.Config = append(out.Config, setting{Key: k.Key, Title: k.Title, Required: k.Required})
	}
	d.mu.Lock()
	if chk, ok := d.checks[s.Name]; ok {
		out.Check = &chk
	}
	d.mu.Unlock()
	return out, nil
}

// errNoPlugin is a plugin the workspace hasn't got: a 404.
var errNoPlugin = errors.New("no such plugin")

// plugin is one of the workspace's plugins, by name.
func (d *dev) plugin(ctx context.Context, name string) (status, error) {
	all, err := d.plugins(ctx)
	if err != nil {
		return status{}, err
	}
	for _, s := range all {
		if s.Name == name {
			return s, nil
		}
	}
	return status{}, fmt.Errorf("the workspace has no plugin called %s: %w", name, errNoPlugin)
}

// answer writes v as JSON.
func answer(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck // the app has gone
}

// refuse writes why not, in plain text.
func refuse(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, errNoPlugin) {
		code = http.StatusNotFound
	}
	http.Error(w, err.Error(), code)
}

// answerPlugin answers with the plugin called name, as it stands now.
func (d *dev) answerPlugin(w http.ResponseWriter, r *http.Request, name string) {
	s, err := d.plugin(r.Context(), name)
	if err != nil {
		refuse(w, err)
		return
	}
	out, err := d.detail(s)
	if err != nil {
		refuse(w, err)
		return
	}
	answer(w, out)
}

// apiPlugins is GET /api/plugins: every plugin the workspace has, and how
// each stands.
func (d *dev) apiPlugins(w http.ResponseWriter, r *http.Request) {
	all, err := d.plugins(r.Context())
	if err != nil {
		refuse(w, err)
		return
	}
	rows := make([]row, len(all))
	for i, s := range all {
		rows[i] = d.row(s)
	}
	answer(w, map[string]any{"plugins": rows})
}

// apiPlugin is GET /api/plugins/{name}: one plugin.
func (d *dev) apiPlugin(w http.ResponseWriter, r *http.Request) {
	d.answerPlugin(w, r, r.PathValue("name"))
}

// apiRestart is POST /api/plugins/{name}/restart: it starts a plugin
// afresh, and answers with the plugin as it then stands.
func (d *dev) apiRestart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == d.p.Name() {
		http.Error(w, "this is the plugin showing you this: ws plugin restart "+name+" starts it afresh", http.StatusConflict)
		return
	}
	if _, err := d.plugin(r.Context(), name); err != nil {
		refuse(w, err)
		return
	}
	if _, err := d.p.WS(r.Context(), "plugin", "restart", name); err != nil {
		refuse(w, err)
		return
	}
	d.answerPlugin(w, r, name)
}

// apiCheck is POST /api/plugins/{name}/check: it runs ws plugin check on
// a plugin, keeps what it said, and answers with the plugin, which has it.
func (d *dev) apiCheck(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s, err := d.plugin(r.Context(), name)
	if err != nil {
		refuse(w, err)
		return
	}
	// Its source if it's linked to it, else what's installed.
	dir := s.Linked
	if dir == "" {
		dir = filepath.Join(d.p.Root(), ".ws", "plugins", name, "app")
	}
	// A check that finds something wrong is still a check: what it found
	// is what's shown.
	out, err := d.p.WS(r.Context(), "plugin", "check", dir)
	said := strings.TrimRight(string(out), "\n")
	if said == "" && err != nil {
		said = err.Error() // it didn't get as far as checking
	}
	d.mu.Lock()
	d.checks[name] = check{At: time.Now(), Output: said, Failed: err != nil}
	d.mu.Unlock()
	d.answerPlugin(w, r, name)
}
