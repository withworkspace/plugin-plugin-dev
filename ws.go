package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/withworkspace/plugin"
)

// dev is the plugin's state, shared by its commands and its pages: what's
// being watched, and what each plugin's last check said.
type dev struct {
	p *plugin.Plugin

	mu       sync.Mutex
	watching map[string]*session // by the plugin's name
	checks   map[string]check    // by the plugin's name
}

// check is what ws plugin check last said of a plugin.
type check struct {
	At     time.Time
	Output string
	Failed bool
}

// added is what ws plugin add -f json prints: how building a plugin and
// running it went.
type added struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Folder      string   `json:"folder"`
	Executable  string   `json:"executable"`
	Built       bool     `json:"built"`
	BuildOutput string   `json:"build_output"`
	Error       string   `json:"error"`
	State       string   `json:"state"`
	Missing     []string `json:"missing"`
}

// status is one of the workspace's plugins, as ws plugin list -f json
// gives it: the parts this plugin shows.
type status struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	State        string   `json:"state"`
	Error        string   `json:"error"`
	Missing      []string `json:"missing"`
	Linked       string   `json:"linked"`
	Starts       int      `json:"starts"`
	Registration struct {
		Description string `json:"description"`
		Views       []struct {
			Title string `json:"title"`
			Path  string `json:"path"`
		} `json:"views"`
		Commands *struct {
			Name        string `json:"name"`
			Short       string `json:"short"`
			Subcommands []struct {
				Name  string `json:"name"`
				Short string `json:"short"`
				Args  string `json:"args"`
			} `json:"subcommands"`
		} `json:"commands"`
		Agent *struct {
			Guide string `json:"guide"`
		} `json:"agent"`
		Config []struct {
			Key      string `json:"key"`
			Title    string `json:"title"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
		} `json:"config"`
	} `json:"registration"`
	Syncs map[string]struct {
		At    time.Time `json:"at"`
		Error string    `json:"error"`
	} `json:"syncs"`
}

// Word is what follows ws to run the plugin's commands; "" if it has none.
func (s status) Word() string {
	if s.Registration.Commands == nil {
		return ""
	}
	if s.Registration.Commands.Name != "" {
		return s.Registration.Commands.Name
	}
	return s.Name
}

// Tone is how its state reads, for a pill.
func (s status) Tone() string {
	switch s.State {
	case "ready":
		return "good"
	case "setup":
		return "warn"
	case "failed":
		return "bad"
	}
	return ""
}

// GuideLines is how long its guide for agents is.
func (s status) GuideLines() int {
	if s.Registration.Agent == nil || strings.TrimSpace(s.Registration.Agent.Guide) == "" {
		return 0
	}
	return strings.Count(strings.TrimSpace(s.Registration.Agent.Guide), "\n") + 1
}

// plugins are the workspace's plugins, in order.
func (d *dev) plugins(ctx context.Context) ([]status, error) {
	out, err := d.p.WS(ctx, "plugin", "list", "-f", "json")
	if err != nil {
		return nil, err
	}
	var all []status
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, fmt.Errorf("ws plugin list -f json: %w", err)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, nil
}

// add builds the plugin in folder and runs what was built in the
// workspace, linked to the folder: ws plugin add --link --build. What it
// returns says how far it got, whatever the error: a plugin that doesn't
// build is one that has a build's output to show.
func (d *dev) add(ctx context.Context, folder string) (added, error) {
	out, err := d.p.WS(ctx, "plugin", "add", folder, "--link", "--build", "-f", "json")
	var res added
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		if err != nil {
			return res, err
		}
		return res, fmt.Errorf("ws plugin add -f json: %w", jerr)
	}
	return res, nil
}

// folder is the plugin's folder a command's about: the one given, else
// where the command was run.
func folder(c *plugin.Call, given string) string {
	switch {
	case given == "":
		return c.Dir()
	case filepath.IsAbs(given):
		return filepath.Clean(given)
	}
	return filepath.Join(c.Dir(), given)
}
