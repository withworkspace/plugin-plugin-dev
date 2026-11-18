// @vitest-environment happy-dom
// @vitest-environment-options { "url": "http://ws.test/plugin-frame/plugin-dev/" }

// The app, run in a page as ws's shell serves it, against ui-lib's fake
// host (ws, without ws) and a stand-in for the plugin's API.

import { fakeHost } from "@withworkspace/ui-lib/testing";
import { afterEach, expect, test, vi } from "vitest";
import { start } from "./app";

const notes = {
  name: "notes",
  version: "0.1.0",
  description: "Notes",
  state: "ready",
  needs: [],
  linked: "/src/plugin-notes",
  adds: ["a view, Notes", "ws notes", "a guide for agents"],
};
const jira = { name: "jira", version: "0.3.0", state: "setup", needs: ["site", "token"], adds: [] };
const detail = {
  ...notes,
  self: false,
  views: ["Notes"],
  commands: [{ usage: "ws notes say [NAME]", short: "Says the greeting" }],
  config: [{ key: "greeting", title: "Greeting", required: false }],
  guide_lines: 2,
  log: "",
};

const closing: { close(): void }[] = [];
afterEach(() => {
  for (const c of closing.splice(0)) c.close();
  vi.unstubAllGlobals();
});

// The page as ws's shell has it, the app started in it at path, and what
// the app asked the plugin for.
async function page(path: string, answers: Record<string, unknown>) {
  history.replaceState(null, "", `/plugin-frame/plugin-dev${path}`);
  document.body.innerHTML = '<div id="root"></div>';
  const asked: string[] = [];
  vi.stubGlobal("fetch", async (url: string, init?: RequestInit) => {
    const what = `${init?.method ?? "GET"} ${url}`;
    asked.push(what);
    const body = answers[what];
    return body === undefined ? new Response(`nothing answers ${what}`, { status: 404 }) : Response.json(body);
  });
  const host = fakeHost({ name: "plugin-dev", path });
  closing.push(host);
  const ws = await start({ host: host.window });
  if (ws) closing.push(ws);
  const root = document.getElementById("root")!;
  return { host, ws, root, asked, answers };
}

test("the list says how each plugin stands, and a plugin's name goes to it", async () => {
  const { host, root, asked } = await page("/", {
    "GET /plugins/plugin-dev/api/plugins": { plugins: [jira, notes] },
    "GET /plugins/plugin-dev/api/plugins/notes": detail,
  });
  expect((await host.next("ws:title")).title).toBe("Plugins");
  await vi.waitFor(() => expect(root.querySelectorAll("tbody tr")).toHaveLength(2));
  const rows = [...root.querySelectorAll("tbody tr")].map((tr) => tr.textContent);
  expect(rows[0]).toContain("jira 0.3.0");
  expect(rows[0]).toContain("needs site, token");
  expect(rows[1]).toContain("a view, Notes · ws notes · a guide for agents");
  expect(rows[1]).toContain("linked to /src/plugin-notes");
  expect(root.querySelector(".pill.warn")?.textContent).toBe("setup");

  // Its name is a link to its own screen, at ws's address for it: a click
  // asks ws to go there, and the app follows when ws says it has. The
  // frame's own address follows too, adding no history.
  const link = [...root.querySelectorAll("a")].find((a) => a.textContent === "notes")!;
  expect(link.getAttribute("href")).toBe("/x/plugin-dev/p/notes");
  const length = history.length;
  link.click();
  expect((await host.next("ws:go")).path).toBe("/x/plugin-dev/p/notes");
  await vi.waitFor(() => expect(location.pathname).toBe("/plugin-frame/plugin-dev/p/notes"));
  expect(history.length).toBe(length);
  await vi.waitFor(() => expect(root.textContent).toContain("ws notes say [NAME]"));
  expect(asked).toEqual(["GET /plugins/plugin-dev/api/plugins", "GET /plugins/plugin-dev/api/plugins/notes"]);
  await vi.waitFor(() => expect(host.sent.filter((m) => m.type === "ws:title").at(-1)).toEqual({ type: "ws:title", title: "notes · Plugins" }));
});

test("a plugin's screen, loaded at its own address, checks it and asks again when told it's stale", async () => {
  const { host, root, asked, answers } = await page("/p/notes", {
    "GET /plugins/plugin-dev/api/plugins/notes": detail,
    "POST /plugins/plugin-dev/api/plugins/notes/check": { ...detail, check: { at: new Date().toISOString(), output: "notes 0.1.0\nNothing's wrong with it.", failed: false } },
  });
  await vi.waitFor(() => expect(root.querySelector("h1")?.textContent).toBe("notes 0.1.0"));
  for (const want of ["Says the greeting", "greeting", "2 lines", "It's logged nothing.", "Linked to /src/plugin-notes"]) expect(root.textContent).toContain(want);

  const button = (label: string) => [...root.querySelectorAll("button")].find((b) => b.textContent === label)!;
  expect(button("Restart")).toBeDefined();
  button("Check").click();
  await vi.waitFor(() => expect(root.textContent).toContain("Nothing's wrong with it."));
  expect(root.querySelector(".pill.good")?.textContent).toBe("ready");
  expect(asked.at(-1)).toBe("POST /plugins/plugin-dev/api/plugins/notes/check");

  // ws:changed: the plugin said what this shows is stale, as it does
  // when a build of a watched plugin finishes.
  answers["GET /plugins/plugin-dev/api/plugins/notes"] = { ...detail, watch: { builds: 2, at: new Date().toISOString(), ok: false, output: "./main.go:3: nope" } };
  host.changed();
  await vi.waitFor(() => expect(root.textContent).toContain("./main.go:3: nope"));
  expect(root.textContent).toContain("What was running still is.");

  // ws moved its address (the sidebar's entry, say): the app follows.
  answers["GET /plugins/plugin-dev/api/plugins"] = { plugins: [] };
  host.navigate("/");
  await vi.waitFor(() => expect(root.textContent).toContain("No plugins yet"));
  expect(location.pathname).toBe("/plugin-frame/plugin-dev/");
});

test("this plugin has no Restart, and what the plugin refuses is said", async () => {
  const { root } = await page("/p/plugin-dev", { "GET /plugins/plugin-dev/api/plugins/plugin-dev": { ...detail, name: "plugin-dev", self: true } });
  await vi.waitFor(() => expect(root.querySelector("button")).not.toBeNull());
  expect([...root.querySelectorAll("button")].map((b) => b.textContent)).toEqual(["Check"]);
  root.querySelector("button")!.click();
  await vi.waitFor(() => expect(root.querySelector(".notice.error")?.textContent).toContain("nothing answers POST"));

  const gone = await page("/p/nobody", {});
  await vi.waitFor(() => expect(gone.root.querySelector(".notice.error")?.textContent).toContain("nothing answers GET /plugins/plugin-dev/api/plugins/nobody"));
});

test("with a ws that speaks no frame protocol it does, the page says which to update", async () => {
  document.body.innerHTML = '<div id="root"></div>';
  const host = fakeHost({ name: "plugin-dev", versions: [2] });
  closing.push(host);
  vi.spyOn(console, "error").mockImplementation(() => {});
  expect(await start({ host: host.window })).toBeUndefined();
  expect(document.getElementById("root")!.textContent).toBe("plugin-dev speaks frame protocol 1, and this ws speaks 2. Update the plugin.");
});
