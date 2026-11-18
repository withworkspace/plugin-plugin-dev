package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/withworkspace/plugin/plugintest"
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

// fakeNPM puts a script on the PATH that stands in for npm, and writes
// each thing it's asked to calls, a line each. install makes
// node_modules; run build writes dist/app.js, unless the app's source
// has a file called broken.
func fakeNPM(t *testing.T) (calls string) {
	t.Helper()
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	body := `#!/bin/sh
echo "$*" >> ` + calls + `
case "$1" in
install)
	mkdir -p node_modules
	;;
run)
	if [ -e src/broken ]; then
		echo "src/app.ts(3,1): error TS1005: nope"
		exit 2
	fi
	mkdir -p dist
	echo "// built" > dist/app.js
	;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
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

	// Its view is an app, built into ui/dist and committed: what ws is
	// told to load is there, served as what it is.
	if reg.UI == nil || !strings.HasPrefix(reg.UI.Script, "/ui/app.js?v=") || !strings.HasPrefix(reg.UI.Style, "/ui/app.css?v=") {
		t.Fatalf("the app registered: %+v", reg.UI)
	}
	if js := ws.Get(reg.UI.Script); js.Status != 200 || !strings.HasPrefix(js.Header.Get("Content-Type"), "text/javascript") {
		t.Errorf("the app's entry: %d, %q", js.Status, js.Header.Get("Content-Type"))
	}
	if css := ws.Get(reg.UI.Style); css.Status != 200 || !strings.HasPrefix(css.Header.Get("Content-Type"), "text/css") {
		t.Errorf("the app's stylesheet: %d, %q", css.Status, css.Header.Get("Content-Type"))
	}
	// It serves no page: where its app's screens are is ws's to serve.
	for _, path := range []string{"/", "/p/notes"} {
		if page := ws.Get(path); page.Status != 404 || strings.Contains(page.Header.Get("Content-Type"), "html") {
			t.Errorf("it answered at %s: %d, %q", path, page.Status, page.Header.Get("Content-Type"))
		}
	}
}

func TestNew(t *testing.T) {
	dir := t.TempDir()
	ws := plugintest.New(t, files, setup, plugintest.Dir(dir))

	out := ws.Run("new", "my-notes", "--no-build", "--no-module")
	if out.Exit != 0 {
		t.Fatalf("new: exit %d\n%s%s", out.Exit, out.Stdout, out.Stderr)
	}
	into := filepath.Join(dir, "plugin-my-notes")
	for _, f := range []string{
		"plugin.toml", "main.go", "main_test.go", "agents.md", ".gitignore", "README.md", "migrations/0001_visits.sql",
		"ui/package.json", "ui/tsconfig.json", "ui/src/main.ts", "ui/src/app.ts", "ui/src/app.css", "ui/src/app.test.ts",
		".github/workflows/release.yml", ".github/workflows/pr.yml",
	} {
		if _, err := os.Stat(filepath.Join(into, f)); err != nil {
			t.Errorf("it didn't write %s", f)
		}
	}
	read := func(f string) string {
		data, _ := os.ReadFile(filepath.Join(into, f))
		return string(data)
	}
	// Its name's filled in, everywhere it's written.
	if got := read("plugin.toml"); !strings.Contains(got, `name    = "my-notes"`) || !strings.Contains(got, `"ws-my-notes"`) {
		t.Errorf("plugin.toml:\n%s", got)
	}
	filepath.WalkDir(into, func(path string, e fs.DirEntry, err error) error { //nolint:errcheck // a test's own folder
		if err == nil && !e.IsDir() {
			if data, _ := os.ReadFile(path); strings.Contains(string(data), "[[") {
				t.Errorf("%s has a part of the template left in it", path)
			}
		}
		return nil
	})
	// Its UI is an app, which the plugin serves the built files of: it
	// has no pages, and nothing that renders one.
	if got := read("main.go"); !strings.Contains(got, `p.UI("ui/dist")`) || !strings.Contains(got, `p.View("My notes", plugin.Icon("sparkle"))`) || !strings.Contains(got, "//go:embed agents.md migrations ui/dist") {
		t.Errorf("main.go:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(into, "views")); err == nil {
		t.Error("it wrote a folder of pages")
	}
	if got := read("ui/src/app.ts"); !strings.Contains(got, `from "@withworkspace/ui-lib"`) || !strings.Contains(got, `title: "My notes"`) {
		t.Errorf("ui/src/app.ts:\n%s", got)
	}
	// Its app is built with what this plugin's own app is: the same
	// ui-lib, and the same of everything else.
	var own, its struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	data, _ := files.ReadFile("ui/package.json")
	if err := json.Unmarshal(data, &own); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(read("ui/package.json")), &its); err != nil {
		t.Fatalf("ui/package.json: %v\n%s", err, read("ui/package.json"))
	}
	if own.Dependencies["@withworkspace/ui-lib"] == "" || !maps(own.Dependencies, its.Dependencies) || !maps(own.DevDependencies, its.DevDependencies) {
		t.Errorf("its app's dependencies are %v and %v, and this plugin's own are %v and %v", its.Dependencies, its.DevDependencies, own.Dependencies, own.DevDependencies)
	}
	// Its workflows are GitHub's own ${{ }}, left as they are, and a pull
	// request checks that the app as built is what's committed.
	if got := read(".github/workflows/release.yml"); !strings.Contains(got, "${{ github.token }}") || !strings.Contains(got, `tags: ["v*"]`) {
		t.Errorf("release.yml:\n%s", got)
	}
	if got := read(".github/workflows/pr.yml"); !strings.Contains(got, "npm ci") || !strings.Contains(got, "npm test") || !strings.Contains(got, "git status --porcelain -- ui/dist") {
		t.Errorf("pr.yml:\n%s", got)
	}
	if got := read(".gitignore"); !strings.Contains(got, "\nnode_modules/\n") || strings.Contains(got, "\nui/dist") {
		t.Errorf(".gitignore:\n%s", got)
	}
	if !strings.Contains(out.Stdout, "ws plugin-dev watch") {
		t.Errorf("it didn't say what's next:\n%s", out.Stdout)
	}

	// A folder with things in it is left alone, and a name that can't be
	// one is refused.
	if out := ws.Run("new", "my-notes", "--no-build", "--no-module"); out.Exit == 0 || !strings.Contains(out.Stderr, "has things in it already") {
		t.Errorf("into a folder that's there: exit %d, %q", out.Exit, out.Stderr)
	}
	if out := ws.Run("new", "Not OK", "--no-build", "--no-module"); out.Exit == 0 {
		t.Error("a name with a space and capitals was taken")
	}
	if out := ws.Run("new"); out.Exit != 2 {
		t.Errorf("with no name: exit %d", out.Exit)
	}
}

func maps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// new builds the new plugin's app, since the plugin is built with it: npm
// install, then npm run build, in ui/. Where that fails it says so, and
// what to run.
func TestNewBuildsTheApp(t *testing.T) {
	calls := fakeNPM(t)
	dir := t.TempDir()
	ws := plugintest.New(t, files, setup, plugintest.Dir(dir))

	out := ws.Run("new", "notes", "--no-module")
	if out.Exit != 0 || !strings.Contains(out.Stdout, "  npm install\n  npm run build\n") || strings.Contains(out.Stdout, "isn't built") {
		t.Errorf("new: exit %d\n%s%s", out.Exit, out.Stdout, out.Stderr)
	}
	if got := readCalls(t, calls); !slices.Equal(got, []string{"install", "run build"}) {
		t.Errorf("it asked npm: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugin-notes", "ui", "dist", "app.js")); err != nil {
		t.Error("its app wasn't built into ui/dist")
	}

	// An app that doesn't build: the plugin's still written, and it says
	// what npm said and what to run.
	t.Setenv("PATH", brokenNPM(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out = ws.Run("new", "broken", "--no-module")
	for _, want := range []string{"Its app isn't built: npm install failed", "npm error nope", "npm run build", "ws plugin-dev watch"} {
		if !strings.Contains(out.Stdout, want) {
			t.Errorf("with an npm that fails, it didn't say %q:\n%s", want, out.Stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "plugin-broken", "main.go")); err != nil || out.Exit != 0 {
		t.Errorf("the plugin wasn't written: exit %d, %v", out.Exit, err)
	}
}

// brokenNPM is a folder with an npm in it that fails at whatever it's
// asked.
func brokenNPM(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh\necho 'npm error nope'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// What new writes is a plugin that builds, with an app that builds, and
// each passes its own test. It needs Go, npm and the network, so it's
// left out of a short run.
func TestNewBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an app and a Go module: not in a short run")
	}
	dir := t.TempDir()
	ws := plugintest.New(t, files, setup, plugintest.Dir(dir))
	out := ws.Run("new", "notes")
	if out.Exit != 0 || strings.Contains(out.Stdout, "isn't set up") || strings.Contains(out.Stdout, "isn't built") {
		t.Skipf("its app or its module couldn't be set up here (no npm, or no network?):\n%s", out.Stdout)
	}
	into := filepath.Join(dir, "plugin-notes")
	for _, f := range []string{"ui/dist/app.js", "ui/dist/app.css", "ui/package-lock.json", "go.mod"} {
		if _, err := os.Stat(filepath.Join(into, f)); err != nil {
			t.Errorf("new left no %s", f)
		}
	}
	// It requires the framework at the version this plugin does.
	if mod, _ := os.ReadFile(filepath.Join(into, "go.mod")); frameworkVersion() != "latest" && !strings.Contains(string(mod), frameworkModule+" "+frameworkVersion()) {
		t.Errorf("its go.mod doesn't require %s %s:\n%s", frameworkModule, frameworkVersion(), mod)
	}
	for _, run := range [][]string{{"go", "vet", "./..."}, {"go", "test", "./..."}, {"npm", "test", "--prefix", "ui"}} {
		cmd := exec.Command(run[0], run[1:]...)
		cmd.Dir = into
		if text, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("the new plugin's own %s: %v\n%s", strings.Join(run, " "), err, text)
		}
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

// ask asks the plugin's HTTP as its app would, and reads the JSON it
// answers into v.
func ask(t *testing.T, res plugintest.Response, v any) {
	t.Helper()
	if kind := res.Header.Get("Content-Type"); res.Status != 200 || kind != "application/json" {
		t.Fatalf("it answered %d, as %q:\n%s", res.Status, kind, res.Body)
	}
	if err := json.Unmarshal([]byte(res.Body), v); err != nil {
		t.Fatalf("%v:\n%s", err, res.Body)
	}
}

// What the app asks: the workspace's plugins, one of them, and to check
// or restart one.
func TestAPI(t *testing.T) {
	script, calls := fakeWS(t)
	ws := plugintest.New(t, files, setup, plugintest.Name("plugin-dev"), plugintest.Executable(script))

	var list struct{ Plugins []row }
	ask(t, ws.Get("/api/plugins"), &list)
	if len(list.Plugins) != 2 || list.Plugins[0].Name != "jira" || list.Plugins[1].Name != "notes" {
		t.Fatalf("the list: %+v", list.Plugins)
	}
	jira, notes := list.Plugins[0], list.Plugins[1]
	if jira.State != "setup" || !slices.Equal(jira.Needs, []string{"site", "token"}) || len(jira.Adds) != 0 {
		t.Errorf("jira: %+v", jira)
	}
	if !slices.Equal(notes.Adds, []string{"a view, Notes", "ws notes", "a guide for agents"}) || notes.Linked != "/src/plugin-notes" || notes.Description != "Notes" || notes.Watch != nil {
		t.Errorf("notes: %+v", notes)
	}

	var one detail
	ask(t, ws.Get("/api/plugins/notes"), &one)
	if one.Name != "notes" || one.Self || one.GuideLines != 2 || one.Log != "" || one.Check != nil ||
		!slices.Equal(one.Views, []string{"Notes"}) ||
		!slices.Equal(one.Commands, []command{{Usage: "ws notes say [NAME]", Short: "Says the greeting"}}) ||
		!slices.Equal(one.Config, []setting{{Key: "greeting", Title: "Greeting"}}) {
		t.Errorf("notes, alone: %+v", one)
	}
	// A plugin the workspace hasn't got is said in plain text, as
	// everything that goes wrong is: the plugin serves no HTML.
	if res := ws.Get("/api/plugins/nobody"); res.Status != 404 || !strings.Contains(res.Body, "no plugin called nobody") || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("a plugin the workspace hasn't got: %d, %q\n%s", res.Status, res.Header.Get("Content-Type"), res.Body)
	}

	// Check asks ws, of the plugin's source, and answers with the plugin,
	// which has what ws said from then on.
	ask(t, ws.Post("/api/plugins/notes/check", nil), &one)
	if one.Check == nil || one.Check.Failed || !strings.Contains(one.Check.Output, "Nothing's wrong with it.") {
		t.Errorf("after a check: %+v", one.Check)
	}
	one = detail{}
	ask(t, ws.Get("/api/plugins/notes"), &one)
	if one.Check == nil || one.Check.At.IsZero() {
		t.Errorf("the check wasn't kept: %+v", one.Check)
	}
	ask(t, ws.Post("/api/plugins/notes/restart", nil), &one)
	got := strings.Join(readCalls(t, calls), "\n")
	for _, want := range []string{"plugin check /src/plugin-notes", "plugin restart notes"} {
		if !strings.Contains(got, want) {
			t.Errorf("it didn't ask ws %q:\n%s", want, got)
		}
	}
	// It doesn't restart itself, or a plugin there isn't.
	if res := ws.Post("/api/plugins/plugin-dev/restart", nil); res.Status != 409 {
		t.Errorf("restarting itself: %d\n%s", res.Status, res.Body)
	}
	if res := ws.Post("/api/plugins/nobody/restart", nil); res.Status != 404 {
		t.Errorf("restarting a plugin there isn't: %d\n%s", res.Status, res.Body)
	}
	if strings.Contains(strings.Join(readCalls(t, calls), "\n"), "plugin restart plugin-dev") {
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
	// What the app was last told, for a wait that isn't met to show.
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
				t.Fatalf("waited for %s: %d builds, and the app was last told:\n%s", what, builds(), last)
			}
		}
	}
	waitFor("the first build", func() bool { return builds() == 1 })
	if got := readCalls(t, calls)[0]; got != "plugin add "+src+" --link --build -f json" {
		t.Errorf("it built with: ws %s", got)
	}
	// While it's watched, the app is told so, and how its last build went.
	waitFor("the app to be told it's watched", shows("/api/plugins", `"watch":{"builds":1,`))
	if !strings.Contains(last, `"ok":true`) {
		t.Errorf("the first build isn't said to have gone well:\n%s", last)
	}

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
	waitFor("the app to be told it failed", shows("/api/plugins/notes", "./main.go:3: nope"))
	if !strings.Contains(last, `"ok":false`) || !strings.Contains(last, "go build failed") {
		t.Errorf("the build that failed isn't said to have:\n%s", last)
	}

	// Stopped, it's no longer watched.
	cancel()
	res := <-done
	for _, want := range []string{"building notes", "running", "./main.go:3: nope", "what was running still is"} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("what it printed lacks %q:\n%s", want, res.Stdout)
		}
	}
	waitFor("the app to be told it isn't watched", func() bool { return !strings.Contains(ws.Get("/api/plugins").Body, `"watch"`) })
	// Each build, and stopping, told the open app that what it shows is stale.
	if n := len(ws.Notifications("view/changed")); n < 4 {
		t.Errorf("the open app was told %d times, over three builds and a stop", n)
	}
}

// A plugin's app is built before the plugin when it's a file of the app
// that was saved, and at the start if it has never been built: the
// plugin is built with what's in ui/dist.
func TestWatchBuildsTheApp(t *testing.T) {
	script, calls := fakeWS(t)
	npm := fakeNPM(t)
	src := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plugin.toml", "name = \"notes\"\n")
	write("main.go", "package main\n")
	write("ui/package.json", "{}\n")
	write("ui/src/app.ts", "export {};\n")
	ws := plugintest.New(t, files, setup, plugintest.Name("plugin-dev"), plugintest.Dir(src), plugintest.Executable(script))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan plugintest.Result, 1)
	go func() { done <- ws.RunContext(ctx, "watch") }()

	// What's been asked of ws and of npm, in a line.
	asked := func() string {
		n := 0
		for _, c := range readCalls(t, calls) {
			if strings.HasPrefix(c, "plugin add ") {
				n++
			}
		}
		return strings.Join(readCalls(t, npm), ", ") + "; " + strings.Repeat("add ", n)
	}
	waitFor := func(what, want string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); asked() != want; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				cancel()
				t.Fatalf("waited for %s: it asked %q, not %q", what, asked(), want)
			}
		}
		// Nothing more follows of itself: what a build writes isn't a save.
		time.Sleep(1200 * time.Millisecond)
		if asked() != want {
			cancel()
			t.Fatalf("after %s it went on to ask %q", what, asked())
		}
	}
	// An app that has never been built is built first, with what it's
	// built with fetched.
	waitFor("the first build", "install, run build; add ")
	// A save of the plugin's Go builds the plugin alone.
	write("main.go", "package main\n\n// changed\n")
	waitFor("a build after a save of main.go", "install, run build; add add ")
	// A save of the app's source builds the app, then the plugin.
	write("ui/src/app.ts", "export {};\n// changed\n")
	waitFor("a build after a save of the app", "install, run build, run build; add add add ")
	// An app that doesn't build: the plugin isn't built, and what was
	// running still is.
	write("ui/src/broken", "")
	waitFor("the app failing to build", "install, run build, run build, run build; add add add ")
	if got := ws.Get("/api/plugins/notes").Body; !strings.Contains(got, "its app didn't build") || !strings.Contains(got, "error TS1005: nope") || !strings.Contains(got, `"ok":false`) {
		t.Errorf("the app wasn't told the app's build failed:\n%s", got)
	}

	cancel()
	res := <-done
	for _, want := range []string{"building notes's app", "src/app.ts(3,1): error TS1005: nope", "its app didn't build (npm run build failed): what was running still is"} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("what it printed lacks %q:\n%s", want, res.Stdout)
		}
	}
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
