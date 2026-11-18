// plugin-dev's app: every plugin the workspace has and how each stands
// (/), and one plugin (/p/<name>), with buttons to restart it and to
// check it. ws serves the page this runs in, with its theme already there
// and an empty #root. What it shows, it asks the plugin for (api.go), at
// ws.api("/api/…").
//
// It's a Preact app. ws keeps the history: the screen is whatever
// usePath says, and a link to ws.href(…) is all it takes to move.

import { type Client, type ConnectOptions, connect } from "@withworkspace/ui-lib";
import type { ComponentChildren } from "preact";
import { render } from "preact";
import { useState } from "preact/hooks";
import { ask, said, useAsk, usePath, useTitle } from "./ws";

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

// Pill is a word in a rounded box, coloured by how it reads.
function Pill({ tone = "", children }: { tone?: string; children: ComponentChildren }) {
  return <span class={`pill ${tone}`.trim()}>{children}</span>;
}

function Watching({ w }: { w: Build }) {
  if (!w.builds) return <Pill>building</Pill>;
  return (
    <>
      {w.ok ? <Pill tone="good">watching</Pill> : <Pill tone="bad">build failed</Pill>}
      <br />
      <small>built {ago(w.at)}</small>
    </>
  );
}

function List({ ws }: { ws: Client }) {
  useTitle(ws, "Plugins");
  const { data, failed } = useAsk<{ plugins: Row[] }>(ws, "/api/plugins");
  const plugins = data?.plugins;
  const table = (
    <table>
      <thead>
        <tr>
          <th>Plugin</th>
          <th>Stands</th>
          <th>Adds</th>
          <th>Being written</th>
        </tr>
      </thead>
      <tbody>
        {plugins?.map((p) => (
          <tr key={p.name}>
            <td>
              <a href={ws.href(`/p/${encodeURIComponent(p.name)}`)}>
                <strong>{p.name}</strong>
              </a>{" "}
              <span class="dim">{p.version}</span>
              {p.description && (
                <>
                  <br />
                  <small>{p.description}</small>
                </>
              )}
            </td>
            <td>
              <Pill tone={tones[p.state]}>{p.state}</Pill>
              {p.needs.length > 0 && (
                <>
                  <br />
                  <small>needs {p.needs.join(", ")}</small>
                </>
              )}
              {p.error && (
                <>
                  <br />
                  <small class="error">{p.error}</small>
                </>
              )}
            </td>
            <td>{p.adds.length ? p.adds.join(" · ") : <span class="dim">nothing yet</span>}</td>
            <td>{p.watch ? <Watching w={p.watch} /> : p.linked && <small class="dim">linked to {p.linked}</small>}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
  return (
    <main>
      <div class="head">
        <h1>Plugins</h1>
      </div>
      {failed && <p class="notice error">{failed}</p>}
      {!plugins ? (
        !failed && <p class="dim">Loading…</p>
      ) : plugins.length ? (
        table
      ) : (
        <div class="empty">
          <h2>No plugins yet</h2>
          <p>Start one below.</p>
        </div>
      )}
      <h2>Writing one</h2>
      <p>
        A plugin adds a view here, commands after <code>ws</code>, and a guide that tells agents they're there. This starts one that works as it's written:
      </p>
      <pre>
        <code>{"ws plugin-dev new notes\ncd plugin-notes\nws plugin-dev watch"}</code>
      </pre>
      <p class="dim">
        <code>watch</code> builds it and runs it in this workspace, and again on every save. Its app loads itself afresh, and how each build went shows here.
      </p>
    </main>
  );
}

function One({ ws, name }: { ws: Client; name: string }) {
  useTitle(ws, `${name} · Plugins`);
  const { data: p, failed, set } = useAsk<Detail>(ws, `/api/plugins/${encodeURIComponent(name)}`);
  const [doing, setDoing] = useState(""); // "restart" or "check", while it's being done
  const [refused, setRefused] = useState(""); // why a restart or a check couldn't be done

  // act restarts or checks it: the plugin answers with how it stands
  // afterwards.
  async function act(what: "restart" | "check") {
    setDoing(what);
    setRefused("");
    try {
      set(await ask<Detail>(ws, `/api/plugins/${encodeURIComponent(name)}/${what}`, "POST"));
    } catch (err) {
      setRefused(said(err));
    }
    setDoing("");
  }

  const back = (
    <p>
      <a href={ws.href("/")}>← Plugins</a>
    </p>
  );
  if (!p) return <main>{back}{failed ? <p class="notice error">{failed}</p> : <p class="dim">Loading…</p>}</main>;

  const button = (what: "restart" | "check", label: string, busy: string) => (
    <button type="button" disabled={doing !== ""} onClick={() => void act(what)}>
      {doing === what ? busy : label}
    </button>
  );
  const added = [
    ...p.views.map((title) => (
      <tr key={`v ${title}`}>
        <td>View</td>
        <td>
          <strong>{title}</strong>
        </td>
        <td class="dim">in the sidebar</td>
      </tr>
    )),
    ...p.commands.map((c) => (
      <tr key={`c ${c.usage}`}>
        <td>Command</td>
        <td>
          <code>{c.usage}</code>
        </td>
        <td class="dim">{c.short}</td>
      </tr>
    )),
    ...p.config.map((k) => (
      <tr key={`k ${k.key}`}>
        <td>Config</td>
        <td>
          <code>{k.key}</code>
        </td>
        <td class="dim">{k.required ? `${k.title}, required` : k.title}</td>
      </tr>
    )),
    ...(p.guide_lines
      ? [
          <tr key="guide">
            <td>Guide</td>
            <td>{count(p.guide_lines, "line")}</td>
            <td class="dim">told to agents</td>
          </tr>,
        ]
      : []),
  ];
  const w = p.watch;
  return (
    <main>
      {back}
      <div class="head">
        <h1>
          {p.name} <span class="dim">{p.version}</span>
        </h1>
        <Pill tone={tones[p.state]}>{p.state}</Pill>
        <span class="grow" />
        {!p.self && button("restart", "Restart", "Restarting…")}
        {button("check", "Check", "Checking…")}
      </div>
      {refused && <p class="notice error">{refused}</p>}
      {failed && <p class="notice error">{failed}</p>}
      {p.description && <p>{p.description}</p>}
      {p.needs.length > 0 && (
        <p class="notice">
          It needs setting up: <code>ws plugin config {p.name} --ask</code> <span class="dim">({p.needs.join(", ")})</span>
        </p>
      )}
      {p.error && <p class="notice error">{p.error}</p>}
      {p.linked && (
        <p class="dim">
          Linked to <code>{p.linked}</code>
        </p>
      )}
      {w && (
        <>
          <h2>Being written</h2>
          {!w.builds && <p>Building…</p>}
          {w.builds > 0 && w.ok && (
            <p>
              <Pill tone="good">built</Pill> {ago(w.at)}, {count(w.builds, "build")} since it's been watched.
            </p>
          )}
          {w.builds > 0 && !w.ok && (
            <>
              <p>
                <Pill tone="bad">build failed</Pill> {ago(w.at)}. What was running still is.
              </p>
              <pre>
                <code>{w.output || w.error || ""}</code>
              </pre>
            </>
          )}
        </>
      )}
      {p.check && (
        <>
          <h2>Check</h2>
          <p>
            {p.check.failed ? <Pill tone="bad">something's wrong</Pill> : <Pill tone="good">checked</Pill>} {ago(p.check.at)}
          </p>
          <pre>
            <code>{p.check.output}</code>
          </pre>
        </>
      )}
      <h2>What it adds</h2>
      {added.length ? (
        <table>
          <tbody>{added}</tbody>
        </table>
      ) : (
        <p class="dim">Nothing yet: it hasn't been started, or it registers nothing.</p>
      )}
      <h2>Log</h2>
      {p.log ? (
        <pre>
          <code>{p.log}</code>
        </pre>
      ) : (
        <p class="dim">It's logged nothing.</p>
      )}
    </main>
  );
}

function Nowhere({ ws, path }: { ws: Client; path: string }) {
  useTitle(ws, "Plugins");
  return (
    <main>
      <h1>Nothing here</h1>
      <p>
        There's nothing at {path}. <a href={ws.href("/")}>Plugins</a>
      </p>
    </main>
  );
}

// App is the screen at the path ws says the app is at.
function App({ ws }: { ws: Client }) {
  const path = usePath(ws).split("?")[0] ?? "/";
  const one = /^\/p\/([^/]+)$/.exec(path);
  if (one) {
    const name = decodeURIComponent(one[1]!);
    return <One key={name} ws={ws} name={name} />;
  }
  return path === "/" ? <List ws={ws} /> : <Nowhere ws={ws} path={path} />;
}

/**
 * start connects to ws and renders the app into the page's #root. It
 * resolves with the client once the app's rendered (each screen then asks
 * the plugin for what it shows), or with nothing where there's no ws to
 * connect to, which the page then says.
 */
export async function start(options: ConnectOptions = {}): Promise<Client | undefined> {
  let ws: Client;
  try {
    ws = await connect(options);
  } catch (err) {
    // No ws to talk to, or one that speaks another frame protocol: the
    // error says which, and what to update.
    render(
      <main>
        <p class="error">{said(err)}</p>
      </main>,
      document.getElementById("root") ?? document.body,
    );
    return undefined;
  }
  render(<App ws={ws} />, ws.root);
  return ws;
}
