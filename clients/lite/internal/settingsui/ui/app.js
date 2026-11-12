// NeoCloud desktop app window: welcome and sign-in, home (status, folders,
// activity), the add-folder wizard, folder editing and settings. Talks only
// to the local server (/api/*), which holds the token; polls /api/state.
"use strict";

// ---------- Helpers ----------

const ICONS = {
  folder: '<path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H9l2 2.5h7.5A2.5 2.5 0 0 1 21 10v7.5a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/>',
  cloud: '<path d="M7 18.5a4.5 4.5 0 0 1-.6-8.96A6 6 0 0 1 18 9.5a4.5 4.5 0 0 1-.5 9z"/>',
  sync: '<path d="M20 11a8 8 0 0 0-14.3-4.9L4 8"/><path d="M4 3.5V8h4.5"/><path d="M4 13a8 8 0 0 0 14.3 4.9L20 16"/><path d="M20 20.5V16h-4.5"/>',
  pause: '<rect x="6.5" y="5" width="3.5" height="14" rx="1"/><rect x="14" y="5" width="3.5" height="14" rx="1"/>',
  play: '<path d="M7 5.5v13a1 1 0 0 0 1.5.86l11-6.5a1 1 0 0 0 0-1.72l-11-6.5A1 1 0 0 0 7 5.5z"/>',
  edit: '<path d="M4 20h4L19 9a2.83 2.83 0 0 0-4-4L4 16z"/><path d="M13.5 6.5l4 4"/>',
  trash: '<path d="M4 7h16"/><path d="M9 7V4.5h6V7"/><path d="M6 7l1 12.5A1.5 1.5 0 0 0 8.5 21h7a1.5 1.5 0 0 0 1.5-1.5L18 7"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h0a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h0a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v0a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  up: '<path d="M12 19V5M6 11l6-6 6 6"/>',
  down: '<path d="M12 5v14M6 13l6 6 6-6"/>',
  both: '<path d="M8 20V4M4 8l4-4 4 4"/><path d="M16 4v16M12 16l4 4 4-4"/>',
  chevron: '<path d="M9 6l6 6-6 6"/>',
  back: '<path d="M15 6l-6 6 6 6"/>',
  external: '<path d="M14 4h6v6"/><path d="M20 4l-9 9"/><path d="M18 14v4.5a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 4 18.5v-11A1.5 1.5 0 0 1 5.5 6H10"/>',
  check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
  alert: '<path d="M12 3.5l9.5 16.5h-19z"/><path d="M12 10v4.5"/><path d="M12 17.5v.01"/>',
  lock: '<rect x="5" y="10.5" width="14" height="10" rx="2"/><path d="M8 10.5V7.5a4 4 0 0 1 8 0v3"/>',
  file: '<path d="M14 3.5H7.5A1.5 1.5 0 0 0 6 5v14a1.5 1.5 0 0 0 1.5 1.5h9A1.5 1.5 0 0 0 18 19V7.5z"/><path d="M14 3.5v4h4"/>',
  copy: '<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5.5A1.5 1.5 0 0 0 14.5 4h-9A1.5 1.5 0 0 0 4 5.5v9A1.5 1.5 0 0 0 5.5 16H8"/>',
  folderPlus: '<path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H9l2 2.5h7.5A2.5 2.5 0 0 1 21 10v7.5a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/><path d="M12 11v5M9.5 13.5h5"/>',
};

function icon(name) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "1.8");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  svg.setAttribute("aria-hidden", "true");
  svg.innerHTML = ICONS[name];
  return svg;
}

// el("div", { class: "x", onclick: fn }, child, "text", …). Falsy children are skipped.
function el(tag, attrs, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (value === undefined || value === null || value === false) continue;
    if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "value") node.value = value;
    else if (value === true) node.setAttribute(key, "");
    else node.setAttribute(key, value);
  }
  for (const child of children.flat(Infinity)) {
    if (child === undefined || child === null || child === false || child === "") continue;
    node.append(child);
  }
  return node;
}

function button(label, onclick, opts = {}) {
  return el("button", { type: "button", class: "btn " + (opts.kind || ""), onclick, title: opts.title, disabled: opts.disabled },
    opts.icon ? icon(opts.icon) : null, label);
}

function iconButton(name, title, onclick, kind = "") {
  return el("button", { type: "button", class: "icon-btn " + kind, title, "aria-label": title, onclick }, icon(name));
}

function dot(kind) { return el("span", { class: "dot " + kind }); }

async function call(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: { "X-Requested-With": "neocloud", "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText || "Request failed");
  return data;
}

let toastTimer = 0;
function toast(message, bad = false) {
  const box = document.getElementById("toast");
  box.textContent = message;
  box.className = "toast" + (bad ? " bad" : "");
  box.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { box.hidden = true; }, bad ? 6000 : 3000);
}

// Runs an action, shows its error as a toast and refreshes the state.
async function act(fn, done) {
  try {
    await fn();
    if (done) toast(done);
  } catch (err) {
    toast(err.message, true);
  }
  refresh();
}

// A small in-page dialog; resolves true when confirmed.
function ask({ title, body, ok, danger }) {
  return new Promise((resolve) => {
    const close = (answer) => { scrim.remove(); document.removeEventListener("keydown", onKey); resolve(answer); };
    const onKey = (event) => { if (event.key === "Escape") close(false); };
    const confirm = button(ok, () => close(true), { kind: danger ? "danger" : "primary" });
    const scrim = el("div", { class: "scrim", onclick: (event) => { if (event.target === scrim) close(false); } },
      el("div", { class: "dialog", role: "dialog", "aria-modal": "true" },
        el("h3", {}, title),
        el("p", {}, body),
        el("div", { class: "actions" }, button("Cancel", () => close(false), { kind: "ghost" }), confirm)));
    document.body.append(scrim);
    document.addEventListener("keydown", onKey);
    confirm.focus();
  });
}

function seen(time) {
  return time && !time.startsWith("0001-");
}

function ago(time) {
  if (!seen(time)) return "";
  const seconds = (Date.now() - new Date(time).getTime()) / 1000;
  if (seconds < 45) return "just now";
  if (seconds < 3600) return Math.round(seconds / 60) + " min ago";
  const date = new Date(time);
  const sameDay = date.toDateString() === new Date().toDateString();
  return sameDay
    ? date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
    : date.toLocaleDateString([], { month: "short", day: "numeric" });
}

function bytes(n) {
  if (n < 1024) return n + " B";
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let value = n;
  let unit = -1;
  do { value /= 1024; unit++; } while (value >= 1024 && unit < units.length - 1);
  return (value >= 10 ? value.toFixed(0) : value.toFixed(1)) + " " + units[unit];
}

// "/Users/me/Documents/Work" → "~/Documents/Work".
function tilde(path) {
  const home = S.home || "";
  if (home && (path === home || path.startsWith(home + "/") || path.startsWith(home + "\\"))) {
    return "~" + path.slice(home.length);
  }
  return path;
}

function baseName(path) {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts.length ? parts[parts.length - 1] : path;
}

function remoteLabel(remote) {
  return remote ? "NeoCloud/" + remote : "NeoCloud";
}

function host(server) {
  try { return new URL(server).host; } catch { return server; }
}

const DIRECTIONS = {
  both: { icon: "both", title: "Both ways", text: "Changes on either side are copied to the other. Best for a folder you work in." },
  up: { icon: "up", title: "Upload only", text: "Back up this computer's folder. Changes made on the server are not downloaded." },
  down: { icon: "down", title: "Download only", text: "Keep a copy of the server folder here. Local changes are not uploaded." },
};

// ---------- State ----------

let S = null;              // last /api/state answer
let quota = null;          // last /api/account answer
let view = { name: "home" };
let painted = "";          // signature of what is on screen
let sawPending = false;    // a sign-in was pending in this window
let closing = false;

function go(next) {
  view = next;
  painted = "";
  paint();
  window.scrollTo(0, 0);
}

async function refresh() {
  try {
    S = await call("GET", "/api/state");
  } catch (err) {
    if (!S) {
      document.getElementById("app").replaceChildren(el("div", { class: "welcome" },
        el("div", { class: "brand" }, el("img", { src: "/assets/logo.svg", alt: "" }), el("h1", {}, "NeoCloud"),
          el("p", { class: "lead" }, "This window lost its connection to NeoCloud. Open it again from the tray icon."))));
    }
    return;
  }
  if (S.superseded && !closing) {
    closing = true;
    window.close(); // a newer window took over; works for app windows
  }
  if (S.login && S.login.pending) sawPending = true;
  if (S.loggedIn && sawPending) {
    sawPending = false;
    view = { name: "connected" };
  }
  if (S.loggedIn && !quota) refreshQuota();
  paint();
}

let quotaLoading = false;
async function refreshQuota() {
  if (!S || !S.loggedIn || quotaLoading) return;
  quotaLoading = true;
  try { quota = await call("GET", "/api/account"); paint(); } catch { /* offline: keep the last one */ }
  quotaLoading = false;
}

// Repaints when what the view shows changed. Forms are left alone while
// the user is typing in them.
function paint() {
  if (!S) return;
  let name = view.name;
  if (!S.loggedIn) name = S.login && S.login.pending ? "approve" : "welcome";
  else if (name === "welcome" || name === "approve") name = view.name = "home";
  const signature = name + "|" + signatureOf(name);
  if (signature === painted) return;
  const typing = document.activeElement && document.activeElement.matches("input, textarea") &&
    document.getElementById("app").contains(document.activeElement);
  if (typing && painted.startsWith(name + "|")) return;
  painted = signature;
  const screens = { welcome, approve, connected, home, add: addFolder, edit: editFolder, issues, settings };
  document.getElementById("app").replaceChildren(...[].concat(screens[name]()));
}

function signatureOf(name) {
  switch (name) {
    case "welcome": return JSON.stringify([S.server, S.login && S.login.error]);
    case "approve": return JSON.stringify(S.login);
    case "connected": return S.username;
    case "home": return JSON.stringify([S.status, S.pairs, S.paused, S.activity, S.keyLock, quota, Math.floor(Date.now() / 30000)]);
    case "add": return JSON.stringify([view.flow, view.step, view.local, view.manual, view.remote && view.remote.folder, view.direction, view.busy, view.newFolders, view.tree && view.tree.rev]);
    case "edit": return JSON.stringify([view.id, S.pairs.find((p) => p.id === view.id), view.rev, view.tree && view.tree.rev]);
    case "issues": return JSON.stringify([view.id, S.status.pairs && S.status.pairs[view.id]]);
    case "settings": return JSON.stringify([S.autostart, S.paused, S.global, S.keyLock, S.username, S.server, S.deviceName, S.logs, view.keyBusy, view.rev]);
  }
  return "";
}

// ---------- Screens: sign-in ----------

function brand(lead) {
  return el("div", { class: "brand" },
    el("img", { src: "/assets/logo.svg", alt: "", width: "64", height: "64" }),
    el("h1", {}, "NeoCloud"),
    el("p", { class: "lead" }, lead));
}

function welcome() {
  const input = el("input", {
    class: "input", id: "server", type: "url", required: true, autocomplete: "url", spellcheck: "false",
    placeholder: "https://cloud.example.com", value: S.server || "",
  });
  const error = el("div", { class: "error" }, (S.login && S.login.error) || "");
  const submit = button("Continue", null, { kind: "primary big wide" });
  submit.type = "submit";
  const form = el("form", {
    onsubmit: async (event) => {
      event.preventDefault();
      error.textContent = "";
      submit.disabled = true;
      try {
        await call("POST", "/api/login", { server: input.value });
        sawPending = true;
      } catch (err) {
        error.textContent = err.message;
      }
      submit.disabled = false;
      refresh();
    },
  },
  el("label", { class: "field", for: "server" }, "Server address", input),
  error,
  submit,
  el("p", { class: "hint" }, "You will approve this computer once in NeoCloud. Your password never goes through this app."));
  setTimeout(() => input.focus(), 0);
  return el("div", { class: "welcome" }, brand("Keep folders on this computer in sync with your NeoCloud."), form);
}

function approve() {
  const login = S.login;
  return el("div", { class: "welcome" },
    el("div", { class: "brand" },
      el("h1", {}, "Approve this computer"),
      el("p", { class: "lead" }, "Check that NeoCloud shows this code, then approve the sign-in.")),
    el("div", { class: "card code-card" },
      el("div", { class: "code selectable", "aria-label": "Sign-in code" }, login.userCode),
      button("Open in browser to approve", () => act(() => call("POST", "/api/open/approval")), { kind: "primary big wide", icon: "external" }),
      login.qrCode ? el("img", { class: "qr", src: login.qrCode, alt: "QR code of the approval link", width: "168", height: "168" }) : null,
      login.qrCode ? el("p", { class: "hint" }, "Or scan it with a phone that is signed in to NeoCloud.") : null,
      el("div", { class: "waiting" }, el("span", { class: "spinner" }), "Waiting for approval…")),
    button("Use a different server", () => act(() => call("DELETE", "/api/login")), { kind: "ghost" }));
}

function connected() {
  return el("div", { class: "welcome" },
    el("div", { class: "brand" },
      el("div", { class: "done-mark" }, icon("check")),
      el("h1", {}, "You're connected"),
      el("p", { class: "lead" }, "Signed in as ", el("strong", {}, S.username), " on ", host(S.server), ".")),
    el("div", { class: "choices" },
      button("Choose what to sync", () => go(newWizard()), { kind: "primary big wide", icon: "folderPlus" }),
      button("Not now", () => go({ name: "home" }), { kind: "ghost wide" })));
}

// ---------- Screens: home ----------

function topBar(title, sub, ...right) {
  return el("header", { class: "bar" },
    el("img", { class: "logo", src: "/assets/logo.svg", alt: "" }),
    el("h1", {}, title, sub ? el("span", { class: "sub ellipsis" }, sub) : null),
    ...right);
}

function backBar(title, onBack) {
  return el("header", { class: "bar" },
    iconButton("back", "Back", onBack || (() => go({ name: "home" }))),
    el("h1", {}, title));
}

function pairOf(id) { return S.pairs.find((pair) => pair.id === id); }
function report(pair) { return (S.status.pairs || {})[pair.id] || null; }

// The overall status: [dot, word, detail].
function overall() {
  const state = S.status.state;
  const attention = S.pairs.some((pair) => { const r = report(pair); return r && (r.error || r.needsConfirmation); });
  if (state === "locked") return ["warn", "Locked", "Insert your security key to sync"];
  if (S.paused || state === "paused") return ["", "Paused", ""];
  if (state === "syncing") {
    const n = S.status.changes;
    return ["busy", n ? "Syncing " + n + (n === 1 ? " file" : " files") : "Syncing", ""];
  }
  if (state === "offline") return ["", "Offline", "Can't reach the server; retrying"];
  if (state === "error" || state === "needs-confirmation" || attention) return ["bad", "Needs attention", ""];
  return ["ok", "Up to date", seen(S.status.lastSync) ? "Checked " + ago(S.status.lastSync) : ""];
}

function pairStatus(pair) {
  const r = report(pair);
  if (S.status.state === "locked") return ["warn", "Locked"];
  if (S.paused || pair.paused) return ["", "Paused"];
  if (r && r.held) return ["", r.held];
  if (r && r.needsConfirmation) return ["bad", "Needs attention"];
  if (r && r.error) return ["bad", S.status.state === "offline" ? "Offline" : "Error"];
  if (S.status.state === "syncing") return ["busy", "Syncing"];
  if (r && r.failed && r.failed.length) return ["bad", r.failed.length + " not synced"];
  if (r && r.notSynced && r.notSynced.length) return ["warn", r.notSynced.length + " left out"];
  if (r && seen(r.lastSync)) return ["ok", "Up to date"];
  return ["", "Waiting"];
}

function home() {
  return [
    topBar("NeoCloud", null,
      iconButton("sync", "Sync now", () => act(() => call("POST", "/api/sync"), "Syncing all folders")),
      iconButton("gear", "Settings", () => go({ name: "settings" }))),
    el("main", { class: "content" },
      overviewCard(),
      ...attentionCards(),
      el("div", { class: "section-head" },
        el("h2", {}, "Folders"),
        button("Add folder", () => go(newWizard()), { kind: "ghost", icon: "plus" })),
      folderList(),
      el("div", { class: "section-head" }, el("h2", {}, "Recent activity")),
      activityList()),
  ];
}

function overviewCard() {
  const [kind, word, detail] = overall();
  const used = quota ? quota.used : 0;
  const limit = quota ? quota.limit : 0;
  let storage = null;
  if (quota) {
    const bar = el("i", {});
    const share = limit > 0 ? Math.min(used / limit, 1) : 0;
    bar.style.width = (limit > 0 ? share * 100 : 100) + "%";
    if (share > 0.9) bar.className = "full";
    storage = el("div", { class: "storage" },
      el("div", { class: "label" },
        el("span", {}, bytes(used) + " used"),
        el("span", {}, limit > 0 ? "of " + bytes(limit) : "No limit")),
      limit > 0 ? el("div", { class: "meter" }, bar) : null);
  }
  const paused = S.paused;
  return el("section", { class: "card overview" },
    el("div", { class: "top" },
      el("div", { class: "avatar" }, (S.username || "?").slice(0, 1)),
      el("div", { class: "who" },
        el("strong", { class: "ellipsis" }, S.username),
        el("span", { class: "ellipsis" }, host(S.server), " · ", S.deviceName))),
    storage,
    el("div", { class: "statusline" },
      el("span", { class: "status" }, dot(kind), word, detail ? el("span", { class: "detail ellipsis" }, "· " + detail) : null),
      button(paused ? "Resume" : "Pause", () => act(() => call("PUT", "/api/settings", { paused: !paused })),
        { icon: paused ? "play" : "pause" })));
}

function attentionCards() {
  const cards = [];
  if (S.status.state === "locked") {
    cards.push(attention("lock", "Insert your security key",
      S.status.message + ". Syncing resumes as soon as the key is back.", [], null, "warn"));
  }
  for (const pair of S.pairs) {
    const r = report(pair);
    if (!r) continue;
    const name = baseName(pair.local);
    const pending = r.needsConfirmation;
    if (pending) {
      const why = pending.reason === "remoteEmpty" ? "The server folder is suddenly empty." : "This sync would delete many files at once.";
      const parts = [];
      if (pending.remoteDeletes) parts.push(pending.remoteDeletes + " on the server");
      if (pending.localDeletes) parts.push(pending.localDeletes + " on this computer");
      const where = pending.localDeletes ? " Files deleted here go to .neocloud/trash for 30 days." : " Files deleted on the server go to its trash.";
      cards.push(attention("alert", "Deletions in " + name + " are waiting",
        why + " Nothing was deleted yet: " + parts.join(" and ") + " would go." + where,
        [
          button("Delete them", () => confirmDeletes(pair, pending), { kind: "danger" }),
          button("Open folder", () => act(() => call("POST", "/api/pairs/" + pair.id + "/open"))),
          pair.paused ? null : button("Pause folder", () => act(() => call("PUT", "/api/pairs/" + pair.id, { paused: true }))),
        ]));
    } else if (r.error && S.status.state !== "offline") {
      cards.push(attention("alert", name + " could not sync", r.error, [
        button("Try again", () => act(() => call("POST", "/api/pairs/" + pair.id + "/sync"), "Syncing " + name), { icon: "sync" }),
        button("Edit", () => go({ name: "edit", id: pair.id })),
      ]));
    }
    for (const folder of r.newFolders || []) {
      const decide = (sync) => act(() => call("POST", "/api/pairs/" + pair.id + "/new-folders", { path: folder.path, sync }), sync ? "Syncing " + folder.path : "Left out " + folder.path);
      cards.push(attention("folderPlus", "New folder in " + remoteLabel(pair.remote) + ": " + folder.path,
        (folder.size ? bytes(folder.size) + ". " : "") + "It is not downloaded to " + name + " until you say so.",
        [button("Sync it", () => decide(true), { kind: "primary" }), button("Don't sync", () => decide(false))], null, "warn"));
    }
    const failed = (r.failed || []).length;
    const skipped = (r.notSynced || []).length;
    if (failed || skipped) {
      const title = failed
        ? failed + (failed === 1 ? " file" : " files") + " in " + name + " could not sync"
        : skipped + (skipped === 1 ? " item" : " items") + " in " + name + " left out";
      const text = failed ? "Retry them, or pick which version to keep." : "These were left alone on purpose (too big, links, names this system can't hold…).";
      cards.push(attention("alert", title, text, [button("Show", () => go({ name: "issues", id: pair.id }))], null, failed ? "" : "warn"));
    }
  }
  return cards;
}

function attention(iconName, title, text, actions, extra, tone = "") {
  return el("section", { class: "card attention " + tone, role: "alert" },
    el("h3", {}, icon(iconName), title),
    el("p", {}, text),
    extra,
    actions.filter(Boolean).length ? el("div", { class: "actions" }, actions) : null);
}

async function confirmDeletes(pair, pending) {
  const total = pending.remoteDeletes + pending.localDeletes;
  const yes = await ask({
    title: "Delete " + total + (total === 1 ? " file?" : " files?"),
    body: [
      pending.remoteDeletes ? pending.remoteDeletes + " in " + remoteLabel(pair.remote) + " (to the NeoCloud trash)." : "",
      pending.localDeletes ? pending.localDeletes + " in " + baseName(pair.local) + " on this computer (to .neocloud/trash for 30 days)." : "",
    ].filter(Boolean).join(" "),
    ok: "Delete", danger: true,
  });
  if (yes) act(() => call("POST", "/api/pairs/" + pair.id + "/confirm-deletes"), "Deleting");
}

function folderList() {
  if (!S.pairs.length) {
    return el("section", { class: "card empty" },
      el("p", {}, "No folders yet. Sync a folder from this computer, or bring your NeoCloud files here."),
      button("Add a folder", () => go(newWizard()), { kind: "primary", icon: "plus" }));
  }
  return el("section", { class: "card folders" }, S.pairs.map((pair) => {
    const [kind, word] = pairStatus(pair);
    const r = report(pair);
    const paused = pair.paused;
    const when = r && seen(r.lastSync) && kind === "ok" ? " · " + ago(r.lastSync) : "";
    return el("div", { class: "folder" },
      el("div", { class: "ficon" }, icon("folder")),
      el("div", { class: "info" },
        el("div", { class: "local ellipsis" }, baseName(pair.local)),
        el("div", { class: "path ellipsis", title: pair.local }, tilde(pair.local)),
        el("div", { class: "route", title: DIRECTIONS[pair.direction].title },
          icon(DIRECTIONS[pair.direction].icon), el("span", { class: "ellipsis" }, remoteLabel(pair.remote))),
        el("div", { class: "state" }, dot(kind), word + when)),
      el("div", { class: "tools" },
        iconButton("folder", "Open in " + fileManager(), () => act(() => call("POST", "/api/pairs/" + pair.id + "/open"))),
        iconButton("sync", "Sync now", () => act(() => call("POST", "/api/pairs/" + pair.id + "/sync"), "Syncing " + baseName(pair.local))),
        iconButton(paused ? "play" : "pause", paused ? "Resume" : "Pause", () => act(() => call("PUT", "/api/pairs/" + pair.id, { paused: !paused }))),
        iconButton("edit", "Edit", () => go({ name: "edit", id: pair.id }))));
  }));
}

function sidebarTitle() {
  return { darwin: "Show in the Finder sidebar", windows: "Show in File Explorer" }[S.os] || "Show in the file manager sidebar";
}

function fileManager() {
  return { darwin: "Finder", windows: "Explorer" }[S.os] || "file manager";
}

function activityList() {
  const items = S.activity || [];
  if (!items.length) return el("section", { class: "card activity" }, el("div", { class: "act none" }, "Nothing has changed yet."));
  const words = {
    upload: ["up", "Uploaded"], download: ["down", "Downloaded"],
    "delete-remote": ["trash", "Deleted on the server"], "delete-local": ["trash", "Deleted here"],
    conflict: ["copy", "Kept both versions of"],
  };
  return el("section", { class: "card activity" }, items.map((item) => {
    const [name, verb] = words[item.kind] || ["file", item.kind];
    const pair = pairOf(item.pair);
    return el("div", { class: "act", title: (pair ? tilde(pair.local) + "/" : "") + item.path },
      icon(name),
      el("div", { class: "what ellipsis" }, el("span", { class: "muted" }, verb + " "), baseName(item.path)),
      el("span", { class: "when" }, ago(item.at)));
  }));
}

// ---------- Screens: add folder ----------

// The wizard starts with a choice (step 0) between two flows of three
// steps each: "local" picks an existing folder here, then the cloud
// folder; "cloud" picks the cloud folder, then where NeoCloud creates the
// folder for it (parent "" is the default location).
const FLOWS = {
  local: [() => stepLocal(), () => stepRemote(), () => stepDirection()],
  cloud: [() => stepRemote(), () => stepPlace(), () => stepDirection()],
};

function newWizard() {
  return { name: "add", step: 0, flow: "", local: "", parent: "", manual: false, remote: null, direction: "both", ignore: "", tree: null, newFolders: "" };
}

function addFolder() {
  const back = view.step === 0 ? () => go({ name: "home" }) : () => go({ ...view, step: view.step - 1 });
  if (view.step === 0) return [backBar("Add a folder", back), el("main", { class: "content" }, stepStart())];
  const steps = el("div", { class: "steps", "aria-hidden": "true" }, [1, 2, 3].map((n) => el("i", { class: n <= view.step ? "on" : "" })));
  return [backBar("Add a folder", back), el("main", { class: "content" }, steps, FLOWS[view.flow][view.step - 1]())];
}

function stepStart() {
  const start = (flow) => go({ ...newWizard(), flow, step: 1 });
  const choice = (iconName, title, text, flow) => el("button", { type: "button", class: "choice", onclick: () => start(flow) },
    el("span", { class: "cicon" }, icon(iconName)),
    el("span", {}, el("strong", {}, title), el("span", {}, text)));
  return [
    el("h2", { class: "step-title" }, "What do you want to sync?"),
    el("div", { class: "choices" },
      choice("folder", "A folder from this computer", "Pick a folder you already have and choose where it goes in NeoCloud.", "local"),
      choice("cloud", "Files from NeoCloud", "Pick a cloud folder, or everything. A NeoCloud folder is created here and filled with your files.", "cloud")),
  ];
}

function stepLocal() {
  const error = el("div", { class: "error" });
  const choose = async () => {
    view.busy = true;
    paint();
    try {
      const picked = await call("POST", "/api/pick-folder");
      if (picked.path) view.local = picked.path;
      if (picked.unsupported) {
        view.manual = true;
        toast("No folder dialog on this system; type the path instead.");
      }
    } catch (err) {
      toast(err.message, true);
    }
    view.busy = false;
    painted = "";
    paint();
  };
  let picked;
  if (view.manual) {
    const input = el("input", { class: "input mono", id: "local", spellcheck: "false", placeholder: S.home ? S.home + "/NeoCloud" : "/path/to/folder", value: view.local,
      oninput: (event) => { view.local = event.target.value.trim(); next.disabled = !view.local; } });
    picked = el("label", { class: "field", for: "local" }, "Folder path", input);
  } else if (view.local) {
    picked = el("section", { class: "card picked" },
      el("div", { class: "ficon" }, icon("folder")),
      el("div", { class: "info" }, el("strong", { class: "ellipsis" }, baseName(view.local)), el("div", { class: "muted small ellipsis", title: view.local }, tilde(view.local))),
      button("Change", choose, { disabled: view.busy }));
  } else {
    picked = button(view.busy ? "Waiting for the dialog…" : "Choose folder…", choose, { kind: "primary big wide", icon: "folder", disabled: view.busy });
  }
  const next = button("Next", () => {
    if (!view.local) return;
    go({ ...view, step: 2, remote: null });
  }, { kind: "primary", disabled: !view.local });
  return [
    el("h2", { class: "step-title" }, "Folder on this computer"),
    el("p", { class: "step-lead" }, "Everything inside it will be synced. It must already exist."),
    picked,
    error,
    view.manual ? null : el("button", { type: "button", class: "linkish", onclick: () => go({ ...view, manual: true }) }, "Type a path instead"),
    el("div", { class: "footer" }, el("span"), next),
  ];
}

async function browse(id) {
  try {
    view.remote = await call("GET", "/api/remote/folders" + (id ? "?id=" + encodeURIComponent(id) : ""));
  } catch (err) {
    toast(err.message, true);
  }
  painted = "";
  paint();
}

// The local folder's name, unless the folder or one inside it has it already.
function suggestedName(listing) {
  if (!view.local) return "";
  const name = baseName(view.local);
  const taken = listing.folder.name === name || listing.folders.some((folder) => folder.name === name);
  return taken ? "" : name;
}

function stepRemote() {
  if (!view.remote) {
    browse("");
    return [el("h2", { class: "step-title" }, "Folder in NeoCloud"), el("div", { class: "waiting" }, el("span", { class: "spinner" }), "Loading folders…")];
  }
  const listing = view.remote;
  const crumbs = el("nav", { class: "crumbs", "aria-label": "Path" }, listing.crumbs.map((crumb, i) => [
    i ? icon("chevron") : null,
    el("button", { type: "button", class: i === listing.crumbs.length - 1 ? "here" : "", onclick: () => browse(crumb.id) }, i ? crumb.name : "NeoCloud"),
  ]));
  const rows = listing.folders.length
    ? listing.folders.map((folder) => el("button", { type: "button", class: "row-btn", onclick: () => browse(folder.id) },
      icon("folder"), el("span", { class: "ellipsis" }, folder.name), el("span", { class: "chev" }, icon("chevron"))))
    : [el("div", { class: "none" }, "No folders here.")];
  const name = el("input", { class: "input", placeholder: "New folder name", value: suggestedName(listing) });
  const create = async () => {
    try {
      const made = await call("POST", "/api/remote/folders", { parentId: listing.folder.id, name: name.value });
      await browse(made.id);
    } catch (err) {
      toast(err.message, true);
    }
  };
  name.addEventListener("keydown", (event) => { if (event.key === "Enter") create(); });
  const cloud = view.flow === "cloud";
  return [
    el("h2", { class: "step-title" }, cloud ? "What to bring to this computer" : "Folder in NeoCloud"),
    el("p", { class: "step-lead" }, cloud ? "Open a folder to sync just that one, or stay at NeoCloud to sync everything." : "Open the folder to sync with, or create a new one."),
    el("section", { class: "card" },
      crumbs,
      el("div", { class: "tree" }, rows),
      el("div", { class: "newfolder" }, name, button("Create", create, { icon: "folderPlus" }))),
    el("div", { class: "target" }, icon("cloud"), "Sync with ", el("strong", { class: "ellipsis" }, remoteLabel(listing.folder.path))),
    el("div", { class: "footer" },
      el("span"),
      button(cloud && !listing.folder.path ? "Sync everything" : "Use this folder", () => go({ ...view, step: view.step + 1, local: cloud ? "" : view.local }), { kind: "primary" })),
  ];
}

// Where the cloud flow creates the local folder: the server proposes a
// free name inside view.parent (the default location when empty).
async function suggestPlace() {
  try {
    const query = "?remote=" + encodeURIComponent(view.remote.folder.path) + "&parent=" + encodeURIComponent(view.parent);
    view.local = (await call("GET", "/api/local/suggest" + query)).path;
  } catch (err) {
    view.parent = "";
    toast(err.message, true);
  }
  painted = "";
  paint();
}

function stepPlace() {
  const title = el("h2", { class: "step-title" }, "Folder on this computer");
  if (!view.local) {
    suggestPlace();
    return [title, el("div", { class: "waiting" }, el("span", { class: "spinner" }), "Finding a place…")];
  }
  const change = async () => {
    view.busy = true;
    paint();
    try {
      const picked = await call("POST", "/api/pick-folder?for=parent");
      if (picked.unsupported) toast("No folder dialog on this system; the folder stays in your home folder.");
      if (picked.path) {
        view.parent = picked.path;
        view.local = "";
      }
    } catch (err) {
      toast(err.message, true);
    }
    view.busy = false;
    painted = "";
    paint();
  };
  const remote = view.remote.folder.path;
  if (!view.tree || view.tree.base !== remote) {
    view.tree = newTree(remote, []);
    view.newFolders = remote ? "sync" : "ask";
  }
  return [
    title,
    el("p", { class: "step-lead" }, "NeoCloud creates this folder and downloads ", el("strong", {}, remoteLabel(remote)), " into it."),
    el("section", { class: "card picked" },
      el("div", { class: "ficon" }, icon("folder")),
      el("div", { class: "info" }, el("strong", { class: "ellipsis" }, baseName(view.local)), el("div", { class: "muted small ellipsis", title: view.local }, tilde(view.local))),
      button("Change location", change, { disabled: view.busy })),
    el("details", { class: "advanced", open: view.picking ? true : undefined, ontoggle: (event) => { view.picking = event.target.open; } },
      el("summary", {}, icon("chevron"), "Choose folders"),
      el("div", { class: "body" }, folderPicker(view.tree, view.newFolders, (value) => { view.newFolders = value; painted = ""; paint(); }))),
    el("div", { class: "footer" }, el("span"), button("Next", () => go({ ...view, step: 3 }), { kind: "primary" })),
  ];
}

function directionChoices(current, onPick) {
  return el("div", { class: "choices", role: "radiogroup" }, Object.entries(DIRECTIONS).map(([value, d]) =>
    el("label", { class: "choice" + (value === current ? " on" : "") },
      el("input", { type: "radio", name: "direction", value, checked: value === current, onchange: () => onPick(value) }),
      el("span", { class: "cicon" }, icon(d.icon)),
      el("span", {}, el("strong", {}, d.title), el("span", {}, d.text)))));
}

function ignoreField(value, onInput, title = "Advanced: ignore patterns") {
  const area = el("textarea", { class: "input", rows: "4", spellcheck: "false", placeholder: "*.log\nnode_modules/\nbuild/", oninput: (event) => onInput(event.target.value) });
  area.value = value;
  return el("details", { class: "advanced", open: value ? true : undefined },
    el("summary", {}, icon("chevron"), title),
    el("div", { class: "body" },
      el("p", { class: "hint" }, "One pattern per line, like .gitignore. Always skipped: " + (S.defaultIgnore || []).map((p) => p.replace("\r", "")).join(", ") + "."),
      area));
}

function stepDirection() {
  const busy = view.busy;
  const start = button(busy ? "Starting…" : "Start syncing", async () => {
    view.busy = true;
    paint();
    try {
      const body = { local: view.local, remote: view.remote.folder.path, direction: view.direction, ignore: view.ignore.split("\n") };
      if (view.flow === "cloud") {
        body.create = true;
        body.settings = { excludePaths: view.tree.excluded, newFolders: view.newFolders };
      }
      await call("POST", "/api/pairs", body);
      toast("Syncing " + baseName(view.local));
      go({ name: "home" });
    } catch (err) {
      view.busy = false;
      painted = "";
      paint();
      toast(err.message, true);
    }
    refresh();
  }, { kind: "primary", disabled: busy });
  return [
    el("h2", { class: "step-title" }, "How should it sync?"),
    el("p", { class: "step-lead", title: view.local }, el("strong", {}, baseName(view.local)), " on this computer and ", el("strong", {}, remoteLabel(view.remote.folder.path))),
    directionChoices(view.direction, (value) => { view.direction = value; painted = ""; paint(); }),
    ignoreField(view.ignore, (value) => { view.ignore = value; }),
    el("div", { class: "footer" }, el("span"), start),
  ];
}

// ---------- Folder tree (selective sync) ----------

// A lazily loaded tree of the cloud folders below base (an account path).
// excluded holds the unticked folders, relative to base, as the pair's
// exclude_paths does; rev changes on every edit so the screen repaints.
function newTree(base, excluded) {
  return { base, excluded: [...(excluded || [])], children: {}, open: {}, rev: 0 };
}

function treeChanged(t) {
  t.rev++;
  painted = "";
  paint();
}

async function loadTree(t, rel) {
  t.children[rel] = t.children[rel] || null;
  const path = [t.base, rel].filter(Boolean).join("/");
  try {
    const listing = await call("GET", "/api/remote/folders?path=" + encodeURIComponent(path));
    t.children[rel] = listing.folders.map((f) => ({ name: f.name, rel: rel ? rel + "/" + f.name : f.name }));
  } catch (err) {
    t.children[rel] = [];
    toast(err.message, true);
  }
  treeChanged(t);
}

function inExcluded(t, rel) { return t.excluded.some((ex) => rel === ex || rel.startsWith(ex + "/")); }

function pickFolder(t, rel, on) {
  t.excluded = on ? t.excluded.filter((ex) => ex !== rel) : t.excluded.filter((ex) => !ex.startsWith(rel + "/")).concat(rel);
  treeChanged(t);
}

function folderTree(t) {
  if (!(("") in t.children)) {
    loadTree(t, "");
    return el("div", { class: "waiting" }, el("span", { class: "spinner" }), "Loading folders…");
  }
  if (t.children[""] === null) return el("div", { class: "waiting" }, el("span", { class: "spinner" }), "Loading folders…");
  const rows = (rel, depth) => (t.children[rel] || []).map((f) => {
    const off = inExcluded(t, f.rel);
    const box = el("input", { type: "checkbox", checked: !off, disabled: rel !== "" && inExcluded(t, rel), onchange: (event) => pickFolder(t, f.rel, event.target.checked) });
    box.indeterminate = !off && t.excluded.some((ex) => ex.startsWith(f.rel + "/"));
    const open = t.open[f.rel];
    const twist = el("button", {
      type: "button", class: "twist" + (open ? " open" : ""), "aria-label": (open ? "Close " : "Open ") + f.name,
      onclick: () => {
        t.open[f.rel] = !open;
        if (!open && !(f.rel in t.children)) loadTree(t, f.rel);
        else treeChanged(t);
      },
    }, icon("chevron"));
    const row = el("div", { class: "pick" }, twist, el("label", {}, box, icon("folder"), el("span", { class: "ellipsis" }, f.name)));
    row.style.paddingLeft = 6 + depth * 18 + "px";
    let below = null;
    if (open) below = t.children[f.rel] ? rows(f.rel, depth + 1) : el("div", { class: "none" }, "Loading…");
    return [row, below];
  });
  const list = rows("", 0).flat(Infinity);
  return el("div", { class: "tree" }, list.length ? list : el("div", { class: "none" }, "No folders inside: everything is synced."));
}

const NEW_FOLDERS = [["sync", "Sync them"], ["ask", "Ask me first"], ["skip", "Don't sync them"]];

function selectField(id, value, options, onchange) {
  return el("select", { class: "input", id, onchange: (event) => onchange(event.target.value) },
    options.map(([v, label]) => el("option", { value: v, selected: v === value }, label)));
}

// The tree plus the new-folders choice under it.
function folderPicker(t, newFolders, onNewFolders) {
  return [
    el("section", { class: "card" }, folderTree(t)),
    el("label", { class: "field", for: "newFolders" }, "New folders that appear in the cloud", selectField("newFolders", newFolders, NEW_FOLDERS, onNewFolders)),
    el("p", { class: "hint" }, "Unticked folders stay in the cloud. Their copy on this computer goes to .neocloud/trash."),
  ];
}

// ---------- Screens: edit folder ----------

const PAIR_SETTINGS = ["mode", "excludePaths", "newFolders", "bigFolderPromptMB", "propagateDeletes", "useDefaultIgnores", "includeHidden",
  "maxFileSizeMB", "symlinks", "conflict", "trashRetentionDays", "deleteGuardFiles", "deleteGuardPercent", "respectNetworkRules"];

const CONFLICTS = [["keep_both", "Keep both (a conflict copy)"], ["prefer_local", "Keep this computer's"], ["prefer_remote", "Keep the cloud's"], ["newer", "Keep the newer one"]];

// A number setting: empty means the default (shown as the placeholder).
function numberField(id, label, value, placeholder, onInput) {
  const input = el("input", { class: "input short", id, type: "number", min: "0", placeholder: String(placeholder), value: value === null || value === undefined ? "" : String(value),
    oninput: (event) => onInput(event.target.value === "" ? null : Math.max(0, Math.floor(Number(event.target.value)))) });
  return el("label", { class: "field", for: id }, label, input);
}

function editFolder() {
  const pair = pairOf(view.id);
  if (!pair) { view = { name: "home" }; return home(); }
  if (view.s === undefined) {
    view.direction = pair.direction;
    view.ignore = (pair.ignore || []).join("\n");
    view.s = Object.fromEntries(PAIR_SETTINGS.map((key) => [key, pair[key] === undefined ? null : pair[key]]));
    view.tree = newTree(pair.remote, pair.excludePaths || []);
    view.rev = 0;
  }
  const s = view.s;
  const redraw = () => { view.rev++; painted = ""; paint(); };
  const flag = (key) => s[key] !== false; // unset means on
  const setFlag = (key) => (on) => { s[key] = on; redraw(); };
  const g = S.global;
  const save = () => act(async () => {
    const settings = { ...s, excludePaths: view.tree.excluded, maxFileSizeMB: s.maxFileSizeMB || 0, newFolders: s.newFolders || "", conflict: s.conflict || "", mode: s.mode || "", symlinks: s.symlinks || "" };
    await call("PUT", "/api/pairs/" + pair.id, { direction: view.direction, ignore: view.ignore.split("\n"), settings });
    go({ name: "home" });
  }, "Saved");
  const remove = async () => {
    const yes = await ask({
      title: "Stop syncing this folder?",
      body: tilde(pair.local) + " stops syncing with " + remoteLabel(pair.remote) + ". Files stay where they are on both sides.",
      ok: "Stop syncing", danger: true,
    });
    if (yes) act(async () => { await call("DELETE", "/api/pairs/" + pair.id); go({ name: "home" }); }, "Folder removed");
  };
  return [
    backBar("Edit folder"),
    el("main", { class: "content" },
      el("section", { class: "card picked" },
        el("div", { class: "ficon" }, icon("folder")),
        el("div", { class: "info" },
          el("strong", { class: "ellipsis" }, baseName(pair.local)),
          el("div", { class: "muted small ellipsis", title: pair.local }, tilde(pair.local)),
          el("div", { class: "muted small ellipsis" }, remoteLabel(pair.remote)))),
      directionChoices(view.direction, (value) => { view.direction = value; redraw(); }),
      el("div", { class: "section-head" }, el("h2", {}, "Folders to sync")),
      folderPicker(view.tree, s.newFolders || "sync", (value) => { s.newFolders = value; redraw(); }),
      el("details", { class: "advanced", open: view.advanced ? true : undefined, ontoggle: (event) => { view.advanced = event.target.open; } },
        el("summary", {}, icon("chevron"), "Advanced"),
        el("div", { class: "body" },
          el("section", { class: "card group" },
            settingRow("Delete on the other side too", "Off: a file deleted on one side stays on the other (a backup, or an archive).",
              toggle("propagateDeletes", flag("propagateDeletes"), setFlag("propagateDeletes")), "propagateDeletes"),
            settingRow("Sync hidden files", "Files starting with a dot" + (S.os === "windows" ? ", and files Windows hides." : "."),
              toggle("includeHidden", flag("includeHidden"), setFlag("includeHidden")), "includeHidden"),
            settingRow("Skip system files", "The built-in list: " + (S.defaultIgnore || []).join(", ") + ".",
              toggle("useDefaultIgnores", flag("useDefaultIgnores"), setFlag("useDefaultIgnores")), "useDefaultIgnores"),
            settingRow("Pause with the network rules", "Wait on metered connections and in battery saver, as set in Settings.",
              toggle("respectNetworkRules", flag("respectNetworkRules"), setFlag("respectNetworkRules")), "respectNetworkRules")),
          el("label", { class: "field", for: "conflict" }, "When a file changed on both sides",
            selectField("conflict", s.conflict || "keep_both", CONFLICTS, (value) => { s.conflict = value; redraw(); })),
          el("div", { class: "limits" },
            numberField("maxFileSizeMB", "Largest file (MB, 0 = any)", s.maxFileSizeMB, 0, (v) => { s.maxFileSizeMB = v; }),
            numberField("trashRetentionDays", "Keep local trash (days, 0 = forever)", s.trashRetentionDays, 30, (v) => { s.trashRetentionDays = v; })),
          el("div", { class: "limits" },
            numberField("deleteGuardFiles", "Ask before deleting more than (files)", s.deleteGuardFiles, g.deleteGuardFiles, (v) => { s.deleteGuardFiles = v; }),
            numberField("deleteGuardPercent", "…or more than (%)", s.deleteGuardPercent, g.deleteGuardPercent, (v) => { s.deleteGuardPercent = v; })),
          numberField("bigFolderPromptMB", "Ask before syncing a new folder bigger than (MB, 0 = never)", s.bigFolderPromptMB, g.bigFolderPromptMB, (v) => { s.bigFolderPromptMB = v; }),
          ignoreField(view.ignore, (value) => { view.ignore = value; }, "Ignore patterns"))),
      el("div", { class: "footer" },
        button("Remove", remove, { kind: "danger", icon: "trash" }),
        button("Save", save, { kind: "primary" }))),
  ];
}

// ---------- Screens: issues ----------

function issues() {
  const pair = pairOf(view.id);
  if (!pair) { view = { name: "home" }; return home(); }
  const r = report(pair) || {};
  const resolve = (item, choice, done) => act(() => call("POST", "/api/pairs/" + pair.id + "/resolve", { path: item.path, choice }), done);
  const failed = (r.failed || []).map((item) => el("div", { class: "issue" },
    el("code", { class: "ellipsis selectable", title: item.path }, item.path),
    el("span", { class: "muted small" }, item.detail),
    el("div", { class: "actions" },
      button("Retry", () => resolve(item, "", "Retrying"), { icon: "sync" }),
      item.both ? button("Keep this computer's", () => resolve(item, "local", "Keeping this computer's version")) : null,
      item.both ? button("Keep the cloud's", () => resolve(item, "remote", "Keeping the cloud's version")) : null,
      item.both ? button("Keep both", () => resolve(item, "both", "Keeping both")) : null)));
  const skipped = (r.notSynced || []).map((item) => el("div", { class: "issue" },
    el("code", { class: "ellipsis selectable", title: item.path }, item.path),
    el("span", { class: "muted small" }, item.detail)));
  return [
    backBar("Issues in " + baseName(pair.local)),
    el("main", { class: "content" },
      el("div", { class: "section-head" }, el("h2", {}, "Could not sync")),
      el("section", { class: "card issues" }, failed.length ? failed : el("div", { class: "act none" }, "Nothing failed in the last sync.")),
      el("div", { class: "section-head" }, el("h2", {}, "Left out on purpose")),
      el("section", { class: "card issues" }, skipped.length ? skipped : el("div", { class: "act none" }, "Nothing was left out.")),
      el("div", { class: "footer" },
        button("Open folder", () => act(() => call("POST", "/api/pairs/" + pair.id + "/open")), { icon: "folder" }),
        button("Edit folder", () => go({ name: "edit", id: pair.id })))),
  ];
}

// ---------- Screens: settings ----------

function toggle(id, checked, onchange, disabled) {
  return el("span", { class: "switch" },
    el("input", { type: "checkbox", id, role: "switch", checked, disabled, onchange: (event) => onchange(event.target.checked) }),
    el("i", {}));
}

function settingRow(title, text, control, forId) {
  return el("div", { class: "setting" },
    el("label", { class: "text", for: forId }, el("strong", {}, title), text ? el("span", {}, text) : null),
    control);
}

function settings() {
  const set = (body, done) => act(() => call("PUT", "/api/settings", body), done);
  if (!view.g) {
    view.g = { ...S.global };
    view.rev = 0;
  }
  const g = view.g;
  const setGlobal = (changes, done) => { Object.assign(g, changes); set({ global: { ...S.global, ...changes } }, done); };
  const proxyUrl = el("input", { class: "input mono", id: "proxyUrl", spellcheck: "false", placeholder: "http://proxy.example.com:3128", value: g.proxyUrl || "",
    oninput: (event) => { g.proxyUrl = event.target.value.trim(); } });
  const up = el("input", { class: "input", id: "up", type: "number", min: "0", step: "100", value: String(S.uploadKiBps || 0) });
  const down = el("input", { class: "input", id: "down", type: "number", min: "0", step: "100", value: String(S.downloadKiBps || 0) });
  return [
    backBar("Settings"),
    el("main", { class: "content" },
      el("div", { class: "section-head" }, el("h2", {}, "General")),
      el("section", { class: "card group" },
        settingRow("Start at login", "Start NeoCloud when you sign in to this computer.",
          toggle("autostart", S.autostart, (on) => set({ autostart: on })), "autostart"),
        settingRow("Pause syncing", "Stop all folders until you resume.",
          toggle("paused", S.paused, (on) => set({ paused: on })), "paused"),
        settingRow(sidebarTitle(), "Shows the NeoCloud folder this app created for you.",
          toggle("sidebar", S.global.fileManagerSidebar, (on) => setGlobal({ fileManagerSidebar: on })), "sidebar"),
        settingRow("Notifications", "", selectField("notifications", S.global.notifications,
          [["errors", "Problems only"], ["all", "Everything"], ["off", "Off"]], (value) => setGlobal({ notifications: value })), "notifications")),
      el("div", { class: "section-head" }, el("h2", {}, "Network")),
      el("section", { class: "card group" },
        S.os === "darwin" ? null : settingRow("Pause on metered connections", "Like a phone hotspot or a capped plan.",
          toggle("metered", S.global.pauseOnMetered, (on) => setGlobal({ pauseOnMetered: on })), "metered"),
        settingRow(S.os === "darwin" ? "Pause in Low Power Mode" : "Pause in battery saver", "Folders can opt out in their advanced settings.",
          toggle("battery", S.global.pauseOnBatterySaver, (on) => setGlobal({ pauseOnBatterySaver: on })), "battery"),
        settingRow("Proxy", "", selectField("proxy", g.proxy, [["system", "System settings"], ["none", "No proxy"], ["manual", "Manual"]],
          (value) => { g.proxy = value; if (value !== "manual") setGlobal({ proxy: value }); else { view.rev++; painted = ""; paint(); } }), "proxy"),
        g.proxy === "manual" ? el("div", { class: "setting column" },
          el("label", { class: "field", for: "proxyUrl" }, "Proxy address (http, https or socks5)", proxyUrl),
          el("div", { class: "unit" }, el("span", { class: "ellipsis" }),
            button("Save proxy", () => setGlobal({ proxy: "manual", proxyUrl: g.proxyUrl }, "Proxy saved")))) : null),
      el("div", { class: "section-head" }, el("h2", {}, "Bandwidth")),
      el("section", { class: "card group" },
        el("div", { class: "setting column" },
          el("div", { class: "limits" },
            el("label", { class: "field", for: "up" }, "Upload limit", el("div", { class: "unit" }, up, el("span", {}, "KiB/s"))),
            el("label", { class: "field", for: "down" }, "Download limit", el("div", { class: "unit" }, down, el("span", {}, "KiB/s")))),
          el("div", { class: "unit" },
            el("p", { class: "hint" }, "0 means no limit."),
            el("span", { class: "ellipsis" }),
            button("Save limits", () => set({ uploadKiBps: Number(up.value) || 0, downloadKiBps: Number(down.value) || 0 }, "Limits saved"))))),
      el("details", { class: "advanced" },
        el("summary", {}, icon("chevron"), "Advanced"),
        el("div", { class: "body" },
          el("p", { class: "hint" }, "Defaults for every folder; a folder can set its own."),
          el("div", { class: "limits" },
            numberField("gGuardFiles", "Ask before deleting more than (files)", g.deleteGuardFiles, 500, (v) => { g.deleteGuardFiles = v; }),
            numberField("gGuardPercent", "…or more than (%)", g.deleteGuardPercent, 50, (v) => { g.deleteGuardPercent = v; })),
          numberField("gBigFolder", "Ask before syncing a new folder bigger than (MB, 0 = never)", g.bigFolderPromptMB, 500, (v) => { g.bigFolderPromptMB = v; }),
          el("div", { class: "unit" }, el("span", { class: "ellipsis" }),
            button("Save", () => setGlobal({ deleteGuardFiles: g.deleteGuardFiles, deleteGuardPercent: g.deleteGuardPercent, bigFolderPromptMB: g.bigFolderPromptMB }, "Saved"))))),
      el("div", { class: "section-head" }, el("h2", {}, "Security key")),
      keyLockCard(),
      el("div", { class: "section-head" }, el("h2", {}, "Account")),
      el("section", { class: "card group" },
        el("div", { class: "setting" },
          el("div", { class: "avatar" }, (S.username || "?").slice(0, 1)),
          el("div", { class: "text" }, el("strong", {}, S.username), el("span", { class: "ellipsis" }, host(S.server)), el("span", { class: "ellipsis" }, "This computer: " + S.deviceName)),
          button("Open web app", () => act(() => call("POST", "/api/open/web")), { icon: "external" })),
        el("div", { class: "setting" },
          el("div", { class: "text" }, el("strong", {}, "Sign out"), el("span", {}, "Syncing stops. Your files stay on this computer and in NeoCloud.")),
          button("Sign out", signOut, { kind: "danger" }))),
      el("div", { class: "section-head" }, el("h2", {}, "About")),
      el("section", { class: "card group" },
        el("div", { class: "setting" },
          el("img", { class: "logo", src: "/assets/logo.svg", alt: "", width: "32", height: "32" }),
          el("div", { class: "text" }, el("strong", {}, "NeoCloud Lite"), el("span", { class: "selectable" }, "Version " + S.version)),
          S.logs ? button("Open logs", () => act(() => call("POST", "/api/open/logs")), { icon: "file" }) : null))),
  ];
}

function keyLockCard() {
  const lock = S.keyLock;
  if (!lock.supported) {
    return el("section", { class: "card group" },
      el("div", { class: "setting" },
        el("div", { class: "text" },
          el("strong", { class: "status" }, dot(""), "Not available"),
          el("span", {}, lock.message))));
  }
  const busy = view.keyBusy;
  const pin = el("input", { class: "input", id: "pin", type: "password", autocomplete: "off", placeholder: "Only if your key has one" });
  const run = async () => {
    if (lock.enrolled) {
      const yes = await ask({ title: "Turn off the security key lock?", body: "The device token is stored as before. Your key must be plugged in.", ok: "Turn off", danger: true });
      if (!yes) return;
    }
    view.keyBusy = true;
    paint();
    try {
      if (lock.enrolled) await call("DELETE", "/api/key-lock");
      else await call("POST", "/api/key-lock", { pin: pin.value });
      toast(lock.enrolled ? "Security key lock is off" : "Security key lock is on");
    } catch (err) {
      toast(err.message, true);
    }
    view.keyBusy = false;
    painted = "";
    refresh();
  };
  const [kind, word] = lock.enrolled ? (lock.locked ? ["warn", "Locked"] : ["ok", "On"]) : ["", "Off"];
  return el("section", { class: "card group" },
    el("div", { class: "setting" },
      el("div", { class: "text" },
        el("strong", { class: "status" }, dot(kind), word),
        el("span", {}, lock.enrolled
          ? lock.message + ". NeoCloud syncs while the key is plugged in and locks when you remove it."
          : "Sync only while your FIDO2 security key is plugged in. The device token is then stored encrypted.")),
      button(busy ? "Touch your key…" : (lock.enrolled ? "Turn off" : "Turn on"), run, { kind: lock.enrolled ? "danger" : "primary", disabled: busy })),
    lock.enrolled ? null : el("div", { class: "setting column" },
      el("label", { class: "field", for: "pin" }, "Security key PIN", pin),
      el("p", { class: "hint" }, "Used once to set up the lock, never stored.")));
}

async function signOut() {
  const yes = await ask({ title: "Sign out?", body: "Syncing stops and this computer forgets its sync state. Files stay where they are.", ok: "Sign out", danger: true });
  if (yes) act(async () => { await call("POST", "/api/logout"); quota = null; go({ name: "home" }); });
}

// Keeps one /api/window request open for as long as this window is: when
// the window closes, the tray app sees it and quits the window's browser.
async function holdWindow() {
  for (;;) {
    try {
      await fetch("/api/window", { headers: { "X-Requested-With": "neocloud" } });
    } catch {}
    if (closing) return;
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
}

// ---------- Start ----------

holdWindow();
refresh();
setInterval(refresh, 2000);
setInterval(refreshQuota, 30000);
