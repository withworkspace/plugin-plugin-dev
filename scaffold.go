package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"github.com/withworkspace/plugin"
)

// frameworkModule is the Go module plugins are written with.
const frameworkModule = "github.com/withworkspace/plugin"

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// new is ws plugin-dev new NAME: a new plugin from the template, with its
// Go module set up so it builds.
func (d *dev) new(c *plugin.Call) error {
	name := c.Arg(0)
	if name == "" {
		return plugin.Exit(2, errors.New("which plugin? ws plugin-dev new NAME"))
	}
	dir := folder(c, c.Option("dir"))
	if c.Option("dir") == "" {
		dir = filepath.Join(c.Dir(), "plugin-"+name)
	}
	files, err := scaffold(dir, name)
	if err != nil {
		return err
	}
	out := c.Stdout()
	fmt.Fprintf(out, "Wrote %s\n", dir)
	for _, f := range files {
		fmt.Fprintf(out, "  %s\n", f)
	}
	if !c.Flag("no-module") {
		// Its module, with the framework at the version this plugin was
		// built with: the one the template was written for.
		if err := goModule(c, dir, "plugin-"+name); err != nil {
			fmt.Fprintf(out, "\nIts Go module isn't set up: %v\nIn %s, run:\n  go mod init plugin-%s\n  go get %s@%s\n  go mod tidy\n", err, dir, name, frameworkModule, frameworkVersion())
		}
	}
	fmt.Fprintf(out, "\nNext:\n  cd %s\n  ws plugin-dev watch\n", dir)
	return nil
}

// scaffold writes a new plugin called name into dir, which must not hold
// anything yet. It returns the files it wrote, relative to dir.
func scaffold(dir, name string) ([]string, error) {
	if !nameRE.MatchString(name) {
		return nil, fmt.Errorf("%q can't be a plugin's name: lower-case letters, digits and hyphens, starting with a letter", name)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s has things in it already", dir)
	}
	data := struct{ Name, Title string }{name, title(name)}
	var written []string
	err := fs.WalkDir(files, "scaffold", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		src, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		// [[ ]], as a plugin's pages are Go templates themselves, with {{ }}.
		t, err := template.New(path).Delims("[[", "]]").Parse(string(src))
		if err != nil {
			return err
		}
		var out bytes.Buffer
		if err := t.Execute(&out, data); err != nil {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(path, "scaffold/"), ".tmpl")
		// What starts with a dot is kept here without it: it would be
		// left out of what's built in, or read as this repo's own.
		switch {
		case rel == "gitignore":
			rel = ".gitignore"
		case strings.HasPrefix(rel, "github/"):
			rel = "." + rel
		}
		to := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		written = append(written, rel)
		return os.WriteFile(to, out.Bytes(), 0o644)
	})
	return written, err
}

// title is a plugin's name as a title: "my-notes" is "My notes".
func title(name string) string {
	r := []rune(strings.ReplaceAll(name, "-", " "))
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// frameworkVersion is the version of the framework this plugin was built with,
// which is the one its template is written for; "latest" where it was
// built from a copy of the framework beside it.
func frameworkVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "latest"
	}
	for _, dep := range info.Deps {
		if dep.Path == frameworkModule {
			if dep.Replace != nil || dep.Version == "" || dep.Version == "(devel)" {
				return "latest"
			}
			return dep.Version
		}
	}
	return "latest"
}

// goModule makes the plugin in dir a Go module that builds.
func goModule(c *plugin.Call, dir, module string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return errors.New("go isn't installed here")
	}
	for _, args := range [][]string{{"mod", "init", module}, {"get", frameworkModule + "@" + frameworkVersion()}, {"mod", "tidy"}} {
		fmt.Fprintf(c.Stdout(), "  go %s\n", strings.Join(args, " "))
		cmd := exec.CommandContext(c.Context(), "go", args...)
		cmd.Dir = dir
		if text, err := cmd.CombinedOutput(); err != nil {
			lines := strings.Split(strings.TrimSpace(string(text)), "\n")
			return fmt.Errorf("go %s: %s", strings.Join(args, " "), lines[len(lines)-1])
		}
	}
	return nil
}

var (
	migrationNumber = regexp.MustCompile(`^(\d+)_`)
	notWord         = regexp.MustCompile(`[^a-z0-9]+`)
)

// migration is ws plugin-dev migration NAME: the next file of a plugin's
// database migrations.
func (d *dev) migration(c *plugin.Call) error {
	if c.Arg(0) == "" {
		return plugin.Exit(2, errors.New("what does it do? ws plugin-dev migration add_assignee"))
	}
	file, err := newMigration(folder(c, c.Option("dir")), strings.Join(c.Args(), " "))
	if err != nil {
		return err
	}
	return c.Say(file)
}

// newMigration writes migrations/0003_<name>.sql in the plugin's folder
// dir, numbered after the last that's there. It returns the file,
// relative to dir.
func newMigration(dir, name string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, "plugin.toml")); err != nil {
		return "", fmt.Errorf("%s has no plugin.toml: it isn't a plugin's folder", dir)
	}
	slug := strings.Trim(notWord.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if slug == "" {
		return "", fmt.Errorf("%q isn't a name for a migration: say what it does, as add_assignee", name)
	}
	into := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(into, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(into)
	if err != nil {
		return "", err
	}
	last := 0
	for _, e := range entries {
		if m := migrationNumber.FindStringSubmatch(e.Name()); m != nil && strings.HasSuffix(e.Name(), ".sql") {
			if n, _ := strconv.Atoi(m[1]); n > last {
				last = n
			}
		}
	}
	rel := filepath.Join("migrations", fmt.Sprintf("%04d_%s.sql", last+1, slug))
	text := "-- " + strings.ReplaceAll(slug, "_", " ") + "\n"
	return rel, os.WriteFile(filepath.Join(dir, rel), []byte(text), 0o644)
}

// check is ws plugin-dev check [FOLDER]: what ws says of the plugin, as
// ws says it.
func (d *dev) check(c *plugin.Call) error {
	cmd := d.p.WSCommand(c.Context(), "plugin", "check", folder(c, c.Arg(0)))
	if cmd == nil {
		return plugin.ErrNoWS
	}
	cmd.Stdout, cmd.Stderr = c.Stdout(), io.Discard // ws says "exit status 1" there, which this says by exiting
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return plugin.Exit(exit.ExitCode(), errors.New("ws plugin check found something wrong"))
		}
		return err
	}
	return nil
}
