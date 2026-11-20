package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// frameworkModule is the Go module plugins are written with, and
// apigenTool its command that writes a plugin's app the types of the
// plugin's endpoints (ui/src/api.gen.ts), as a tool of the plugin's
// module.
const (
	frameworkModule = "github.com/withworkspace/plugin"
	apigenTool      = frameworkModule + "/cmd/apigen"
	apiTypes        = "ui/src/api.gen.ts"
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// new is ws plugin-dev new NAME: a new plugin from the template, with its
// app built and its Go module set up, so it builds.
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
	if !c.Flag("no-build") {
		// Its app, built into ui/dist: the plugin is built with what's
		// there, so it doesn't build until its app has been.
		ui := filepath.Join(dir, appFolder)
		fmt.Fprintf(out, "\nIts app, built into %s:\n", appBuilt)
		step := func(args string) { fmt.Fprintf(out, "  npm %s\n", args) }
		if text, err := buildApp(c.Context(), ui, step); err != nil {
			if text != "" {
				fmt.Fprintf(out, "%s\n", indent(text))
			}
			fmt.Fprintf(out, "\nIts app isn't built: %v%s\nThe plugin is built with its app, so it won't build until it is. In %s, run:\n  npm install\n  npm run build\n", err, oldNPM(c.Context()), ui)
		}
	}
	if !c.Flag("no-module") {
		// Its module, with the framework at the version this plugin was
		// built with: the one the template was written for.
		fmt.Fprintln(out, "\nIts Go module:")
		if err := goModule(c, dir, "plugin-"+name); err != nil {
			fmt.Fprintf(out, "\nIts Go module isn't set up: %v\nIn %s, run:\n  go mod init plugin-%s\n  go get %s@%s\n  go get -tool %s@%s\n  go mod tidy\n", err, dir, name, frameworkModule, frameworkVersion(), apigenTool, frameworkVersion())
		}
	}
	fmt.Fprintf(out, "\nNext:\n  cd %s\n  ws plugin-dev watch\n", dir)
	return nil
}

// indent is text, each line set in under what it follows.
func indent(text string) string {
	return "    " + strings.ReplaceAll(text, "\n", "\n    ")
}

// The folder of a plugin's app, and where it's built into.
const (
	appFolder = "ui"
	appBuilt  = "ui/dist"
)

// buildApp builds a plugin's app, in its folder ui, into ui/dist: npm run
// build, after npm install where what it's built with isn't there yet.
// step is told each npm command as it's run. It returns what the command
// that failed printed, with the error.
func buildApp(ctx context.Context, ui string, step func(args string)) (string, error) {
	if _, err := exec.LookPath("npm"); err != nil {
		return "", errors.New("npm isn't installed here (it comes with Node)")
	}
	steps := [][]string{{"run", "build"}}
	if _, err := os.Stat(filepath.Join(ui, "node_modules")); err != nil {
		steps = [][]string{{"install"}, {"run", "build"}}
	}
	for _, args := range steps {
		if step != nil {
			step(strings.Join(args, " "))
		}
		cmd := exec.CommandContext(ctx, "npm", args...)
		cmd.Dir = ui
		if text, err := cmd.CombinedOutput(); err != nil {
			return strings.TrimSpace(string(text)), fmt.Errorf("npm %s failed", strings.Join(args, " "))
		}
	}
	return "", nil
}

// oldNPM says, where the npm here is older than 11, that it's likely why
// the app didn't build: npm 10, which Node 22 comes with, fails to
// install what the app is tested with ("Cannot read properties of
// null"). "" where npm is new enough, or doesn't say.
func oldNPM(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "npm", "--version").Output()
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(string(out))
	major, _, _ := strings.Cut(version, ".")
	if n, err := strconv.Atoi(major); err != nil || n >= 11 {
		return ""
	}
	return "\nThe npm here is " + version + ", and the app needs npm 11 or later, which comes with Node 24."
}

// appDeps are the packages plugin-dev's own app is built with, and the
// version of each: what a new plugin's app is given, so the two move
// together.
func appDeps() (map[string]string, error) {
	data, err := files.ReadFile("ui/package.json")
	if err != nil {
		return nil, err
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("ui/package.json: %w", err)
	}
	deps := map[string]string{}
	for name, version := range pkg.DevDependencies {
		deps[name] = version
	}
	for name, version := range pkg.Dependencies {
		deps[name] = version
	}
	return deps, nil
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
	deps, err := appDeps()
	if err != nil {
		return nil, err
	}
	funcs := template.FuncMap{
		// dep is the version of a package this plugin's own app is built
		// with, for the new app's package.json.
		"dep": func(name string) (string, error) {
			if v := deps[name]; v != "" {
				return v, nil
			}
			return "", fmt.Errorf("plugin-dev's own ui/package.json has no %s to give the new app", name)
		},
	}
	var written []string
	err = fs.WalkDir(files, "scaffold", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		src, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		// [[ ]], as a workflow's own ${{ }} is left as it is.
		t, err := template.New(path).Delims("[[", "]]").Funcs(funcs).Parse(string(src))
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
	// apigen is the framework's own, kept as a tool of the module: it
	// writes the app the types of the plugin's endpoints (go tool apigen).
	for _, args := range [][]string{
		{"mod", "init", module},
		{"get", frameworkModule + "@" + frameworkVersion()},
		{"get", "-tool", apigenTool + "@" + frameworkVersion()},
		{"mod", "tidy"},
	} {
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
