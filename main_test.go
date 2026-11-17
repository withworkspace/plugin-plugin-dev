package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brudil/workspace-kit/plugintest"
)

// fakeWS is a script that answers as ws would to what this plugin asks of
// it, and writes each thing it's asked to calls, a line each. The
// workspace it lists has two plugins; one it adds builds, unless its
// folder has a file called broken.
func fakeWS(t *testing.T) (script, calls string) {
	t.Helper()
	dir := t.TempDir()
	script, calls = filepath.Join(dir, "ws"), filepath.Join(dir, "calls")
	body := `#!/bin/sh
echo "$*" >> ` + calls + `
case "$1 $2" in
"plugin list")
	cat <<'JSON'
[
 {"name":"notes","version":"0.1.0","state":"ready","linked":"/src/plugin-notes","starts":2,
  "registration":{"description":"Notes","views":[{"title":"Notes","path":"/"}],
   "commands":{"short":"Notes","subcommands":[{"name":"say","short":"Says the greeting","args":"[NAME]"}]},
   "agent":{"guide":"one\ntwo"},"config":[{"key":"greeting","title":"Greeting","type":"string"}]}},
 {"name":"jira","version":"0.3.0","state":"setup","missing":["site","token"],"registration":{}}
]
JSON
	;;
"plugin add")
	if [ -e "$3/broken" ]; then
		printf '%s\n' '{"name":"notes","folder":"'"$3"'","built":false,"build_output":"./main.go:3: nope\n","error":"go build failed"}'
		exit 1
	fi
	printf '%s\n' '{"name":"notes","version":"0.1.0","folder":"'"$3"'","executable":"'"$3"'/ws-notes","built":true,"state":"ready"}'
	;;
"plugin check")
	echo "notes 0.1.0"
	echo "Nothing's wrong with it."
	;;
"plugin restart")
	echo "$3 is ready"
	;;
*)
	echo "unexpected: $*" >&2
	exit 9
	;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, calls
}

func readCalls(t *testing.T, calls string) []string {
	t.Helper()
	data, err := os.ReadFile(calls)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestRegisters(t *testing.T) {
	ws := plugintest.New(t, files, setup, plugintest.Name("plugin-dev"))
	reg := ws.Registration()
	if len(reg.Views) != 1 || reg.Views[0].Title != "Plugins" {
		t.Errorf("views = %+v", reg.Views)
	}
	var names []string
	for _, c := range reg.Commands.Subcommands {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, " "); got != "new watch migration check" {
		t.Errorf("commands = %s", got)
	}
	if reg.Agent == nil || !strings.Contains(reg.Agent.Guide, "ws plugin-dev new NAME") {
		t.Errorf("guide = %+v", reg.Agent)
	}
}

func TestNew(t *testing.T) {
	dir := t.TempDir()
	ws := plugintest.New(t, files, setup, plugintest.Dir(dir))

	out := ws.Run("new", "my-notes", "--no-module")
	if out.Exit != 0 {
		t.Fatalf("new: exit %d\n%s%s", out.Exit, out.Stdout, out.Stderr)
	}
	into := filepath.Join(dir, "plugin-my-notes")
	for _, f := range []string{"plugin.toml", "main.go", "main_test.go", "agents.md", ".gitignore", "README.md", "migrations/0001_visits.sql", "views/index.html"} {
		if _, err := os.Stat(filepath.Join(into, f)); err != nil {
			t.Errorf("it didn't write %s", f)
		}
	}
	read := func(f string) string {
		data, _ := os.ReadFile(filepath.Join(into, f))
		return string(data)
	}
	// Its name's filled in, and a page's own {{ }} is left as it is.
	if got := read("plugin.toml"); !strings.Contains(got, `name    = "my-notes"`) || !strings.Contains(got, `"ws-my-notes"`) {
		t.Errorf("plugin.toml:\n%s", got)
	}
	if got := read("main.go"); !strings.Contains(got, `p.View("My notes"`) || strings.Contains(got, "[[") {
		t.Errorf("main.go:\n%s", got)
	}
	if got := read("views/index.html"); !strings.Contains(got, "{{.Greeting}}") || !strings.Contains(got, `{{define "title"}}My notes{{end}}`) {
		t.Errorf("views/index.html:\n%s", got)
	}
	if !strings.Contains(out.Stdout, "ws plugin-dev watch") {
		t.Errorf("it didn't say what's next:\n%s", out.Stdout)
	}

	// A folder with things in it is left alone, and a name that can't be
	// one is refused.
	if out := ws.Run("new", "my-notes", "--no-module"); out.Exit == 0 || !strings.Contains(out.Stderr, "has things in it already") {
		t.Errorf("into a folder that's there: exit %d, %q", out.Exit, out.Stderr)
	}
	if out := ws.Run("new", "Not OK", "--no-module"); out.Exit == 0 {
		t.Error("a name with a space and capitals was taken")
	}
	if out := ws.Run("new"); out.Exit != 2 {
		t.Errorf("with no name: exit %d", out.Exit)
	}
}

// What new writes is a plugin that builds and passes its own test. It
// needs Go and the framework's module, so it's left out of a short run.
func TestNewBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a Go module: not in a short run")
	}
	dir := t.TempDir()
	ws := plugintest.New(t, files, setup, plugintest.Dir(dir))
	out := ws.Run("new", "notes")
	if out.Exit != 0 || strings.Contains(out.Stdout, "isn't set up") {
		t.Skipf("its module couldn't be set up here (no network?):\n%s", out.Stdout)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = filepath.Join(dir, "plugin-notes")
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("the new plugin's own test: %v\n%s", err, text)
	}
}

func TestMigration(t *testing.T) {
	dir := t.TempDir()
	ws := plugintest.New(t, files, setup, plugintest.Dir(dir))
	if out := ws.Run("migration", "add tags"); out.Exit == 0 {
		t.Error("a folder that isn't a plugin's got a migration")
	}
	os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(`name = "x"`), 0o644)
	for i, want := range []string{"migrations/0001_add_tags.sql", "migrations/0002_drop_the_old_table.sql"} {
		name := []string{"add tags", "Drop the old table!"}[i]
		out := ws.Run("migration", name)
		if out.Exit != 0 || strings.TrimSpace(out.Stdout) != want {
			t.Errorf("migration %q: exit %d, %q", name, out.Exit, out.Stdout)
		}
	}
	// Numbered after the last that's there, whatever its number.
	os.WriteFile(filepath.Join(dir, "migrations", "0010_by_hand.sql"), nil, 0o644)
	if out := ws.Run("migration", "next"); strings.TrimSpace(out.Stdout) != "migrations/0011_next.sql" {
		t.Errorf("after 0010: %q", out.Stdout)
	}
}

func TestCheckIsWSs(t *testing.T) {
	script, calls := fakeWS(t)
	dir := t.TempDir()
	ws := plugintest.New(t, files, setup, plugintest.Dir(dir), plugintest.Executable(script))
	out := ws.Run("check")
	if out.Exit != 0 || !strings.Contains(out.Stdout, "Nothing's wrong with it.") {
		t.Errorf("check: exit %d, %q %q", out.Exit, out.Stdout, out.Stderr)
	}
	if got := readCalls(t, calls); len(got) != 1 || got[0] != "plugin check "+dir {
		t.Errorf("it asked ws: %q", got)
	}
}

func TestPages(t *testing.T) {
	script, calls := fakeWS(t)
	ws := plugintest.New(t, files, setup, plugintest.Name("plugin-dev"), plugintest.Executable(script))

	page := ws.Get("/")
	if page.Status != 200 {
		t.Fatalf("the list: %d\n%s", page.Status, page.Body)
	}
	for _, want := range []string{"notes", "a view, Notes · ws notes · a guide for agents", "needs site, token", "pill-warn", "linked to /src/plugin-notes"} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("the list lacks %q:\n%s", want, page.Body)
		}
	}

	page = ws.Get("/p/notes")
	for _, want := range []string{"ws notes say [NAME]", "Says the greeting", "<code>greeting</code>", "2 lines", "It's logged nothing."} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("a plugin's page lacks %q:\n%s", want, page.Body)
		}
	}
	if page := ws.Get("/p/nobody"); page.Status == 200 {
		t.Error("a plugin the workspace hasn't got has a page")
	}

	// Check asks ws, of the plugin's source, and the page says what it said.
	if page := ws.Post("/p/notes/check", nil); page.Status != 303 {
		t.Fatalf("check: %d\n%s", page.Status, page.Body)
	}
	if page := ws.Get("/p/notes"); !strings.Contains(page.Body, "Nothing&#39;s wrong with it.") || !strings.Contains(page.Body, "checked") {
		t.Errorf("after a check:\n%s", page.Body)
	}
	ws.Post("/p/notes/restart", nil)
	got := strings.Join(readCalls(t, calls), "\n")
	for _, want := range []string{"plugin check /src/plugin-notes", "plugin restart notes"} {
		if !strings.Contains(got, want) {
			t.Errorf("it didn't ask ws %q:\n%s", want, got)
		}
	}
	// It doesn't restart itself from its own page.
	if page := ws.Post("/p/plugin-dev/restart", nil); page.Status == 303 {
		t.Error("it restarted itself")
	}
}

func TestWatch(t *testing.T) {
	script, calls := fakeWS(t)
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "plugin.toml"), []byte("name = \"notes\"\n"), 0o644)
	os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644)
	ws := plugintest.New(t, files, setup, plugintest.Name("plugin-dev"), plugintest.Dir(src), plugintest.Executable(script))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan plugintest.Result, 1)
	go func() { done <- ws.RunContext(ctx, "watch") }()

	builds := func() int {
		n := 0
		for _, c := range readCalls(t, calls) {
			if strings.HasPrefix(c, "plugin add ") {
				n++
			}
		}
		return n
	}
	// The page last looked at, for a wait that isn't met to show.
	var last string
	shows := func(path, text string) func() bool {
		return func() bool {
			last = ws.Get(path).Body
			return strings.Contains(last, text)
		}
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				cancel()
				t.Fatalf("waited for %s: %d builds, and the page last looked at was:\n%s", what, builds(), last)
			}
		}
	}
	waitFor("the first build", func() bool { return builds() == 1 })
	if got := readCalls(t, calls)[0]; got != "plugin add "+src+" --link --build -f json" {
		t.Errorf("it built with: ws %s", got)
	}
	// While it's watched, its page says so.
	waitFor("the page to say it's watched", shows("/", "watching"))

	// A save is built. What isn't source isn't.
	os.WriteFile(filepath.Join(src, "notes.db"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(src, ".hidden"), []byte("x"), 0o644)
	time.Sleep(1200 * time.Millisecond)
	if builds() != 1 {
		t.Errorf("a database and a hidden file were built for: %d builds", builds())
	}
	os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n\n// changed\n"), 0o644)
	waitFor("a build after a save", func() bool { return builds() == 2 })

	// A build that fails is said, with what it printed, and shown.
	os.WriteFile(filepath.Join(src, "broken"), nil, 0o644)
	waitFor("a build after a broken save", func() bool { return builds() == 3 })
	waitFor("the page to say it failed", shows("/p/notes", "./main.go:3: nope"))

	// Stopped, it's no longer watched.
	cancel()
	res := <-done
	for _, want := range []string{"building notes", "running", "./main.go:3: nope", "what was running still is"} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("what it printed lacks %q:\n%s", want, res.Stdout)
		}
	}
	waitFor("the page to say it isn't watched", func() bool { return !strings.Contains(ws.Get("/").Body, "build failed") })
}

func TestWatchRefusals(t *testing.T) {
	script, _ := fakeWS(t)
	src := t.TempDir()
	ws := plugintest.New(t, files, setup, plugintest.Name("plugin-dev"), plugintest.Dir(src), plugintest.Executable(script))
	if out := ws.Run("watch"); out.Exit == 0 || !strings.Contains(out.Stderr, "isn't a plugin's folder") {
		t.Errorf("a folder that isn't a plugin's: exit %d, %q", out.Exit, out.Stderr)
	}
	// Itself: running what it built would stop what's watching.
	os.WriteFile(filepath.Join(src, "plugin.toml"), []byte("name = \"plugin-dev\"\n"), 0o644)
	if out := ws.Run("watch"); out.Exit == 0 || !strings.Contains(out.Stderr, "can't watch itself") {
		t.Errorf("watching itself: exit %d, %q", out.Exit, out.Stderr)
	}
}
