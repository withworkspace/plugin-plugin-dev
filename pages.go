package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/withworkspace/plugin"
)

// row is one of the workspace's plugins on the Plugins page.
type row struct {
	status
	Needs string   // the config it requires that isn't set
	Adds  string   // what it adds, in a line
	Watch *session // how its last build went, if it's being watched
}

func (d *dev) row(s status) row {
	r := row{status: s, Needs: strings.Join(s.Missing, ", "), Watch: d.watched(s.Name)}
	var adds []string
	for _, v := range s.Registration.Views {
		adds = append(adds, "a view, "+v.Title)
	}
	if word := s.Word(); word != "" {
		adds = append(adds, "ws "+word)
	}
	if s.GuideLines() > 0 {
		adds = append(adds, "a guide for agents")
	}
	r.Adds = strings.Join(adds, " · ")
	return r
}

// indexPage is every plugin the workspace has, and how each stands.
func (d *dev) indexPage(r *plugin.Request) (any, error) {
	all, err := d.plugins(r.Context())
	if err != nil {
		return nil, err
	}
	rows := make([]row, len(all))
	for i, s := range all {
		rows[i] = d.row(s)
	}
	return map[string]any{"Plugins": rows}, nil
}

// pluginPage is one plugin: what it registers, how its last build and its
// last check went, and the end of its log.
func (d *dev) pluginPage(r *plugin.Request) (any, error) {
	name := r.PathValue("name")
	s, err := d.plugin(r.Context(), name)
	if err != nil {
		return nil, err
	}
	log, err := tail(filepath.Join(d.p.Root(), ".ws", "plugins", name, "data", "plugin.log"), 60)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	chk, checked := d.checks[name]
	d.mu.Unlock()
	data := map[string]any{"Plugin": d.row(s), "Log": log, "Self": name == d.p.Name()}
	if checked {
		data["Check"] = chk
	}
	return data, nil
}

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
	return status{}, fmt.Errorf("the workspace has no plugin called %s", name)
}

// restartPost starts a plugin afresh.
func (d *dev) restartPost(r *plugin.Request) error {
	name := r.PathValue("name")
	if name == d.p.Name() {
		return errors.New("this is the plugin showing you this page: ws plugin restart " + name + " starts it afresh")
	}
	_, err := d.p.WS(r.Context(), "plugin", "restart", name)
	return err
}

// checkPost runs ws plugin check on a plugin, and keeps what it said for
// its page.
func (d *dev) checkPost(r *plugin.Request) error {
	name := r.PathValue("name")
	s, err := d.plugin(r.Context(), name)
	if err != nil {
		return err
	}
	// Its source if it's linked to it, else what's installed.
	dir := s.Linked
	if dir == "" {
		dir = filepath.Join(d.p.Root(), ".ws", "plugins", name, "app")
	}
	out, err := d.p.WS(r.Context(), "plugin", "check", dir)
	d.mu.Lock()
	d.checks[name] = check{At: time.Now(), Output: strings.TrimRight(string(out), "\n"), Failed: err != nil}
	d.mu.Unlock()
	return nil // what it found is on the page
}
