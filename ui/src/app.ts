// plugin-dev's app: every plugin the workspace has and how each stands
// (/), and one plugin (/p/<name>), with buttons to restart it and to
// check it. ws serves the page this runs in, with its theme already there
// and an empty #root. What it shows, it asks the plugin for (api.go), at
// ws.api("/api/…").

import { type Client, type ConnectOptions, connect } from "@withworkspace/ui-lib";

// How the last build of a plugin being watched went.
interface Build {
  builds: number; // 0 while the first is running
  at: string;
  ok: boolean;
  output?: string;
  error?: string;
}

// One of the workspace's plugins, as GET /api/plugins lists it.
interface Row {
  name: string;
  version: string;
  description?: string;
  state: string;
  error?: string;
  needs: string[];
  linked?: string;
  adds: string[];
  watch?: Build;
}

// One plugin, as GET /api/plugins/<name> gives it.
interface Detail extends Row {
  self: boolean;
  views: string[];
  commands: { usage: string; short: string }[];
  config: { key: string; title: string; required: boolean }[];
  guide_lines: number;
  check?: { at: string; output: string; failed: boolean };
  log: string;
}

type Child = Node | string | false | undefined;

// h makes an element: its properties, then what's in it.
function h<K extends keyof HTMLElementTagNameMap>(tag: K, props: Partial<HTMLElementTagNameMap[K]> = {}, ...children: Child[]): HTMLElementTagNameMap[K] {
  const el = Object.assign(document.createElement(tag), props);
  el.append(...children.filter((c) => c !== false && c !== undefined));
  return el;
}

const said = (err: unknown) => (err instanceof Error ? err.message : String(err));

// pill is a word in a rounded box, coloured by how it reads.
function pill(text: string, tone = ""): HTMLElement {
  return h("span", { className: `pill ${tone}`.trim() }, text);
}

// How a plugin's state reads.
const tones: Record<string, string> = { ready: "good", setup: "warn", failed: "bad" };

// ago is a time as how long ago it was: "3 minutes ago".
function ago(at: string): string {
  const seconds = Math.round((Date.parse(at) - Date.now()) / 1000);
  if (Number.isNaN(seconds)) return "";
  if (seconds > -45) return "just now";
  const to = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
  const steps: [Intl.RelativeTimeFormatUnit, number][] = [
    ["minute", 60],
    ["hour", 3600],
    ["day", 86400],
  ];
  let [unit, size] = steps[0]!;
  for (const step of steps) if (-seconds >= step[1]) [unit, size] = step;
  return to.format(Math.round(seconds / size), unit);
}

const count = (n: number, one: string) => `${n} ${n === 1 ? one : `${one}s`}`;

/**
 * start connects to ws and shows the app in the page's #root. It resolves
 * with the client once the first screen is up, or with nothing where
 * there's no ws to connect to, which the page then says.
 */
export async function start(options: ConnectOptions = {}): Promise<Client | undefined> {
  let ws: Client;
  try {
    ws = await connect(options);
  } catch (err) {
    // No ws to talk to, or one that speaks another frame protocol: the
    // error says which, and what to update.
    (document.getElementById("root") ?? document.body).replaceChildren(h("main", {}, h("p", { className: "error" }, said(err))));
    return undefined;
  }

  let plugins: Row[] | undefined; // the list, once asked for
  let detail: Detail | undefined; // the plugin last asked for
  let failed = ""; // why the last ask failed
  let doing = ""; // "restart" or "check", while it's being done
  let refused = ""; // why a restart or a check couldn't be done

  // Which screen ws's address is at.
  type Screen = { at: "list" } | { at: "plugin"; name: string } | { at: "nowhere"; path: string };
  function screen(): Screen {
    const path = ws.path.split("?")[0] ?? "/";
    const one = /^\/p\/([^/]+)$/.exec(path);
    if (one) return { at: "plugin", name: decodeURIComponent(one[1]!) };
    return path === "/" ? { at: "list" } : { at: "nowhere", path };
  }

  // A screen of the app's own, as a link's address.
  const to = (path: string) => ws.base + path;
  const toPlugin = (name: string) => to(`/p/${encodeURIComponent(name)}`);

  // The plugin's own HTTP is at ws.api(path). What goes wrong there is
  // said in plain text.
  async function ask<T>(path: string, method = "GET"): Promise<T> {
    const res = await fetch(ws.api(path), { method });
    if (!res.ok) throw new Error((await res.text()).trim() || `${path} answered ${res.status}`);
    return (await res.json()) as T;
  }

  // load asks for what the screen shows, and shows it.
  async function load() {
    const s = screen();
    try {
      if (s.at === "list") plugins = (await ask<{ plugins: Row[] }>("/api/plugins")).plugins;
      if (s.at === "plugin") detail = await ask<Detail>(`/api/plugins/${encodeURIComponent(s.name)}`);
      failed = "";
    } catch (err) {
      failed = said(err);
    }
    render();
  }

  // act restarts or checks a plugin: the plugin answers with how it
  // stands afterwards.
  async function act(name: string, what: "restart" | "check") {
    [doing, refused] = [what, ""];
    render();
    try {
      detail = await ask<Detail>(`/api/plugins/${encodeURIComponent(name)}/${what}`, "POST");
    } catch (err) {
      refused = said(err);
    }
    doing = "";
    render();
  }

  function watching(w: Build): Child[] {
    if (!w.builds) return [pill("building")];
    return [w.ok ? pill("watching", "good") : pill("build failed", "bad"), h("br"), h("small", {}, `built ${ago(w.at)}`)];
  }

  function list(): { title: string; body: Child[] } {
    const rows = (plugins ?? []).map((p) =>
      h(
        "tr",
        {},
        h("td", {}, h("a", { href: toPlugin(p.name) }, h("strong", {}, p.name)), " ", h("span", { className: "dim" }, p.version), !!p.description && h("br"), !!p.description && h("small", {}, p.description)),
        h("td", {}, pill(p.state, tones[p.state]), p.needs.length > 0 && h("br"), p.needs.length > 0 && h("small", {}, `needs ${p.needs.join(", ")}`), !!p.error && h("br"), !!p.error && h("small", { className: "error" }, p.error)),
        h("td", {}, p.adds.length ? p.adds.join(" · ") : h("span", { className: "dim" }, "nothing yet")),
        h("td", {}, ...(p.watch ? watching(p.watch) : [!!p.linked && h("small", { className: "dim" }, `linked to ${p.linked}`)])),
      ),
    );
    const table = h(
      "table",
      {},
      h("thead", {}, h("tr", {}, h("th", {}, "Plugin"), h("th", {}, "Stands"), h("th", {}, "Adds"), h("th", {}, "Being written"))),
      h("tbody", {}, ...rows),
    );
    const empty = h("div", { className: "empty" }, h("h2", {}, "No plugins yet"), h("p", {}, "Start one below."));
    return {
      title: "Plugins",
      body: [
        h("div", { className: "head" }, h("h1", {}, "Plugins")),
        !!failed && h("p", { className: "notice error" }, failed),
        !plugins ? !failed && h("p", { className: "dim" }, "Loading…") : plugins.length ? table : empty,
        h("h2", {}, "Writing one"),
        h("p", {}, "A plugin adds a view here, commands after ", h("code", {}, "ws"), ", and a guide that tells agents they're there. This starts one that works as it's written:"),
        h("pre", {}, h("code", {}, "ws plugin-dev new notes\ncd plugin-notes\nws plugin-dev watch")),
        h("p", { className: "dim" }, h("code", {}, "watch"), " builds it and runs it in this workspace, and again on every save. Its app loads itself afresh, and how each build went shows here."),
      ],
    };
  }

  function one(name: string): { title: string; body: Child[] } {
    const back = h("p", {}, h("a", { href: to("/") }, "← Plugins"));
    const p = detail?.name === name ? detail : undefined;
    if (!p) return { title: `${name} · Plugins`, body: [back, failed ? h("p", { className: "notice error" }, failed) : h("p", { className: "dim" }, "Loading…")] };

    const button = (what: "restart" | "check", label: string, busy: string) => {
      const b = h("button", { type: "button", disabled: doing !== "" }, doing === what ? busy : label);
      b.addEventListener("click", () => void act(name, what));
      return b;
    };
    const added = [
      ...p.views.map((title) => h("tr", {}, h("td", {}, "View"), h("td", {}, h("strong", {}, title)), h("td", { className: "dim" }, "in the sidebar"))),
      ...p.commands.map((c) => h("tr", {}, h("td", {}, "Command"), h("td", {}, h("code", {}, c.usage)), h("td", { className: "dim" }, c.short))),
      ...p.config.map((k) => h("tr", {}, h("td", {}, "Config"), h("td", {}, h("code", {}, k.key)), h("td", { className: "dim" }, k.required ? `${k.title}, required` : k.title))),
      ...(p.guide_lines ? [h("tr", {}, h("td", {}, "Guide"), h("td", {}, count(p.guide_lines, "line")), h("td", { className: "dim" }, "told to agents"))] : []),
    ];
    const w = p.watch;
    const written: Child[] = !w
      ? []
      : [
          h("h2", {}, "Being written"),
          !w.builds && h("p", {}, "Building…"),
          w.builds > 0 && w.ok && h("p", {}, pill("built", "good"), ` ${ago(w.at)}, ${count(w.builds, "build")} since it's been watched.`),
          w.builds > 0 && !w.ok && h("p", {}, pill("build failed", "bad"), ` ${ago(w.at)}. What was running still is.`),
          w.builds > 0 && !w.ok && h("pre", {}, h("code", {}, w.output || w.error || "")),
        ];
    const checked: Child[] = !p.check
      ? []
      : [h("h2", {}, "Check"), h("p", {}, p.check.failed ? pill("something's wrong", "bad") : pill("checked", "good"), ` ${ago(p.check.at)}`), h("pre", {}, h("code", {}, p.check.output))];

    return {
      title: `${p.name} · Plugins`,
      body: [
        back,
        h("div", { className: "head" }, h("h1", {}, p.name, " ", h("span", { className: "dim" }, p.version)), pill(p.state, tones[p.state]), h("span", { className: "grow" }), !p.self && button("restart", "Restart", "Restarting…"), button("check", "Check", "Checking…")),
        !!refused && h("p", { className: "notice error" }, refused),
        !!failed && h("p", { className: "notice error" }, failed),
        !!p.description && h("p", {}, p.description),
        p.needs.length > 0 && h("p", { className: "notice" }, "It needs setting up: ", h("code", {}, `ws plugin config ${p.name} --ask`), " ", h("span", { className: "dim" }, `(${p.needs.join(", ")})`)),
        !!p.error && h("p", { className: "notice error" }, p.error),
        !!p.linked && h("p", { className: "dim" }, "Linked to ", h("code", {}, p.linked)),
        ...written,
        ...checked,
        h("h2", {}, "What it adds"),
        added.length ? h("table", {}, h("tbody", {}, ...added)) : h("p", { className: "dim" }, "Nothing yet: it hasn't been started, or it registers nothing."),
        h("h2", {}, "Log"),
        p.log ? h("pre", {}, h("code", {}, p.log)) : h("p", { className: "dim" }, "It's logged nothing."),
      ],
    };
  }

  function nowhere(path: string): { title: string; body: Child[] } {
    return { title: "Plugins", body: [h("h1", {}, "Nothing here"), h("p", {}, `There's nothing at ${path}. `, h("a", { href: to("/") }, "Plugins"))] };
  }

  function render() {
    const s = screen();
    const { title, body } = s.at === "list" ? list() : s.at === "plugin" ? one(s.name) : nowhere(s.path);
    ws.setTitle(title);
    ws.root.replaceChildren(h("main", {}, ...body));
  }

  // Where the frame's own address is, as a path in the plugin.
  const here = () => (location.pathname.slice(ws.base.length) || "/") + location.search;

  // moved is the app at another screen, however it got there.
  function moved() {
    [failed, refused] = ["", ""];
    render();
    void load();
  }

  // A link to a screen of the app's own goes there without loading the
  // page again: the frame's history gains an entry, and ws's address
  // follows. (A data-ws link is ui-lib's: it opens in ws.)
  ws.root.addEventListener("click", (e) => {
    const a = (e.target as Element).closest?.("a[href]");
    if (!(a instanceof HTMLAnchorElement) || a.dataset.ws !== undefined || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    const path = a.pathname + a.search;
    if (a.origin !== location.origin || !(path === ws.base || path.startsWith(`${ws.base}/`))) return;
    e.preventDefault();
    history.pushState(null, "", a.href);
    ws.setPath(here());
    moved();
  });
  // Back and forward in the frame's own history: ws's address follows.
  window.addEventListener("popstate", () => {
    ws.setPath(here());
    moved();
  });
  // ws's address moved without the app: the sidebar, a link in ws, back
  // and forward there. The app goes there, replacing its own entry.
  ws.onNavigate((path) => {
    history.replaceState(null, "", ws.base + path);
    moved();
  });
  // The plugin said what this shows is stale (a build finished, say).
  ws.onChanged(() => void load());

  // The frame's own address starts where ws's is.
  if (here() !== ws.path) history.replaceState(null, "", ws.base + ws.path);
  render();
  await load();
  return ws;
}
