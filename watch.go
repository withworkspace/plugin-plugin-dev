package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/withworkspace/plugin"
)

// session is a plugin being watched: its folder, and how its last build
// went. The pages show it, so it's read under dev's lock.
type session struct {
	Name   string
	Folder string
	Since  time.Time

	Builds  int       // how many times it's been built
	At      time.Time // when it last was
	OK      bool      // whether that built and started
	Output  string    // what the build printed
	Error   string    // why it didn't build, or didn't start
	State   string    // where the plugin stands, once started
	Missing []string  // config it requires that isn't set
}

var manifestName = regexp.MustCompile(`(?m)^\s*name\s*=\s*"([^"]+)"`)

// pluginName is the name in the plugin.toml in dir.
func pluginName(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.toml"))
	if err != nil {
		return "", fmt.Errorf("%s has no plugin.toml: it isn't a plugin's folder", dir)
	}
	m := manifestName.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("%s doesn't say the plugin's name", filepath.Join(dir, "plugin.toml"))
	}
	return string(m[1]), nil
}

// watch is ws plugin-dev watch [FOLDER]: build the plugin and run it in
// the workspace, then again whenever a file of it changes, until the
// command's stopped. What the plugin logs is shown as it's written.
func (d *dev) watch(c *plugin.Call) error {
	dir := folder(c, c.Arg(0))
	name, err := pluginName(dir)
	if err != nil {
		return err
	}
	if name == d.p.Name() {
		// Running what was built means stopping what's running, which is
		// what's watching.
		return fmt.Errorf("%s can't watch itself: starting it afresh would stop this. Build it, then ws plugin restart %s", name, name)
	}
	s, err := d.begin(name, dir)
	if err != nil {
		return err
	}
	defer d.end(name)

	out := c.Stdout()
	say := func(format string, args ...any) {
		fmt.Fprintf(out, "%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	}
	ctx := c.Context()

	build := func() string {
		say("building %s", name)
		res, err := d.add(ctx, dir)
		if err != nil && res.Name == "" {
			res.Error = err.Error()
		}
		d.built(s, res)
		switch {
		case res.Error != "" && res.State == "failed":
			say("built, but it didn't start: %s", res.Error)
		case res.Error != "":
			if text := strings.TrimRight(res.BuildOutput, "\n"); text != "" {
				fmt.Fprintln(out, "          "+strings.ReplaceAll(text, "\n", "\n          "))
			}
			say("%s: what was running still is", res.Error)
		case res.State == "setup":
			say("running, and needs setting up: ws plugin config %s %s", name, strings.Join(placeholders(res.Missing), " "))
		default:
			say("running")
		}
		d.p.Changed() // the Plugins page shows how it went
		return res.Executable
	}

	// What it logs, from here on.
	log := &follower{path: filepath.Join(d.p.Root(), ".ws", "plugins", name, "data", "plugin.log")}
	log.skipToEnd()

	exe := build()
	say("watching %s: Ctrl-C to stop", dir)

	// The folder's read on a tick, not listened to: it's small, and this
	// sees every editor's way of saving. A save is often several writes,
	// so a build follows the first tick on which nothing more changed.
	before := snapshot(dir, exe)
	changed := false
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		for _, line := range log.lines() {
			fmt.Fprintf(out, "          %s\n", line)
		}
		now := snapshot(dir, exe)
		if !same(before, now) {
			before, changed = now, true
			continue
		}
		if changed {
			changed = false
			exe = build()
			before = snapshot(dir, exe) // what the build itself wrote isn't a save
		}
	}
}

func placeholders(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k + "=…"
	}
	return out
}

// begin records that a plugin's being watched. One watch of a plugin at
// a time: two would each build over the other.
func (d *dev) begin(name, dir string) (*session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if was := d.watching[name]; was != nil {
		return nil, fmt.Errorf("%s is being watched already, from %s", name, was.Folder)
	}
	s := &session{Name: name, Folder: dir, Since: time.Now()}
	d.watching[name] = s
	return s, nil
}

func (d *dev) end(name string) {
	d.mu.Lock()
	delete(d.watching, name)
	d.mu.Unlock()
	d.p.Changed()
}

// built records how a build went.
func (d *dev) built(s *session, res added) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s.Builds++
	s.At = time.Now()
	s.OK = res.Error == ""
	s.Output, s.Error, s.State, s.Missing = res.BuildOutput, res.Error, res.State, res.Missing
}

// watched is a copy of what's known of a plugin being watched, for a
// page; nil if it isn't.
func (d *dev) watched(name string) *session {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.watching[name]
	if s == nil {
		return nil
	}
	copy := *s
	return &copy
}

// stamp is a file as it was when it was last looked at.
type stamp struct {
	mod  time.Time
	size int64
}

// snapshot is every file of the plugin's source in dir, as it is now. exe
// is what the build writes, which isn't source.
func snapshot(dir, exe string) map[string]stamp {
	out := map[string]stamp{}
	filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error { //nolint:errcheck // what can't be read isn't watched
		if err != nil {
			return nil
		}
		name := e.Name()
		if e.IsDir() {
			if path != dir && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if path == exe || notSource(name) {
			return nil
		}
		if info, err := e.Info(); err == nil {
			out[path] = stamp{info.ModTime(), info.Size()}
		}
		return nil
	})
	return out
}

// notSource says whether a file's changing is no change to the plugin: an
// editor's own files, and a database or a log beside the source.
func notSource(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") {
		return true
	}
	for _, ext := range []string{".swp", ".swx", ".tmp", ".db", ".db-wal", ".db-shm", ".log"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func same(a, b map[string]stamp) bool {
	if len(a) != len(b) {
		return false
	}
	for path, s := range a {
		if b[path] != s {
			return false
		}
	}
	return true
}

// follower reads what's written to a file from where it last read to.
type follower struct {
	path   string
	offset int64
}

func (f *follower) skipToEnd() {
	if info, err := os.Stat(f.path); err == nil {
		f.offset = info.Size()
	}
}

// lines are the whole lines written since it last looked.
func (f *follower) lines() []string {
	file, err := os.Open(f.path)
	if err != nil {
		return nil // not written yet
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() < f.offset {
		f.offset = 0 // it was started over
	}
	if _, err := file.Seek(f.offset, io.SeekStart); err != nil {
		return nil
	}
	var out []string
	r := bufio.NewReader(file)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			break // a line still being written is read next time
		}
		f.offset += int64(len(line))
		out = append(out, strings.TrimRight(line, "\n"))
	}
	return out
}

// tail is the last n lines of the file at path.
func tail(path string, n int) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}
