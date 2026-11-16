// "Send to device": the card on the home screen. Chosen files and dropped
// ones wait in `send.files`; a click on an online device sends them.
// Progress and results come with /api/state (S.send.transfers).
"use strict";

const send = {
  files: [],        // [{ path, name, size }]
  devices: null,    // online devices that can receive, null until asked
  deviceError: "",
  busy: false,      // choosing or adding files
  over: false,      // something is being dragged over the window
};

ICONS.send = '<path d="M21 3 10.5 13.5"/><path d="M21 3l-6.5 18-4-7.5L3 9.5z"/>';
ICONS.phone = '<rect x="7" y="3" width="10" height="18" rx="2.5"/><path d="M11 18h2"/>';
ICONS.computer = '<rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M8.5 20h7M12 16.5V20"/>';
ICONS.globe = '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17"/><path d="M12 3.5c2.5 2.4 3.7 5.2 3.7 8.5s-1.2 6.1-3.7 8.5c-2.5-2.4-3.7-5.2-3.7-8.5S9.5 5.9 12 3.5z"/>';

function sendTotal() { return send.files.reduce((sum, file) => sum + file.size, 0); }

async function refreshSendDevices() {
  if (!S || !S.loggedIn || !S.send || !S.send.enabled || view.name !== "home") return;
  try {
    send.devices = (await call("GET", "/api/send/devices")).devices;
    send.deviceError = "";
  } catch (err) {
    send.devices = null;
    send.deviceError = err.message;
  }
  paint();
}

function addFiles(files) {
  const known = new Set(send.files.map((file) => file.path));
  send.files.push(...files.filter((file) => !known.has(file.path)));
}

async function chooseFiles() {
  send.busy = true;
  painted = "";
  paint();
  try {
    const answer = await call("POST", "/api/send/pick");
    if (answer.unsupported) toast("This system has no file dialog. Drop files on the window instead.", true);
    else if (answer.files) addFiles(answer.files);
  } catch (err) {
    toast(err.message, true);
  }
  send.busy = false;
  painted = "";
  paint();
}

// Files dropped on the window. A browser gives the page the bytes, not the
// path, so each file is handed to the app first (/api/send/stage).
async function dropFiles(transfer) {
  const files = [];
  let folders = 0;
  for (const item of transfer.items || []) {
    const entry = item.webkitGetAsEntry && item.webkitGetAsEntry();
    if (entry && entry.isDirectory) folders++;
    else if (item.kind === "file" && item.getAsFile()) files.push(item.getAsFile());
  }
  if (folders) toast("Folders can't be sent. Put a folder in a zip file first.", true);
  if (!files.length) return;
  send.busy = true;
  painted = "";
  paint();
  for (const file of files) {
    if (file.size > S.send.maxBytes) {
      toast(file.name + " is " + bytes(file.size) + ". You can send up to " + bytes(S.send.maxBytes) + " at once.", true);
      continue;
    }
    try {
      const res = await fetch("/api/send/stage?name=" + encodeURIComponent(file.name), {
        method: "PUT", headers: { "X-Requested-With": "neocloud" }, body: file,
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(humanError(data.error || "The file could not be added"));
      addFiles([data]);
    } catch (err) {
      toast(err.message, true);
    }
  }
  send.busy = false;
  painted = "";
  paint();
}

function removeFile(file) {
  send.files = send.files.filter((other) => other.path !== file.path);
  call("POST", "/api/send/discard", { paths: [file.path] }).catch(() => {});
  painted = "";
  paint();
}

async function sendTo(device) {
  const paths = send.files.map((file) => file.path);
  try {
    await call("POST", "/api/send", { deviceId: device.id, paths });
    send.files = [];
    toast("Sending to " + device.name);
  } catch (err) {
    toast(err.message, true);
  }
  painted = "";
  refresh();
}

// Dropping anywhere on the window adds files; without these handlers the
// browser would open the dropped file instead.
function dragHasFiles(event) { return Array.from(event.dataTransfer.types || []).includes("Files"); }
window.addEventListener("dragover", (event) => {
  if (!dragHasFiles(event)) return;
  event.preventDefault();
  if (!send.over) { send.over = true; document.body.classList.add("dropping"); }
});
window.addEventListener("dragleave", (event) => {
  if (event.relatedTarget === null) { send.over = false; document.body.classList.remove("dropping"); }
});
window.addEventListener("drop", (event) => {
  if (!dragHasFiles(event)) return;
  event.preventDefault();
  send.over = false;
  document.body.classList.remove("dropping");
  if (S && S.loggedIn && S.send && S.send.enabled && view.name === "home") dropFiles(event.dataTransfer);
});

function deviceIcon(device) {
  return icon({ phone: "phone", desktop: "computer", browser: "globe" }[device.kind] || "computer");
}

function sendCard() {
  const total = sendTotal();
  const tooMany = send.files.length > S.send.maxFiles || total > S.send.maxBytes;
  return el("section", { class: "card send" },
    el("div", { class: "drop" },
      el("div", { class: "eicon" }, icon("send")),
      el("div", { class: "text" },
        el("strong", {}, send.files.length ? send.files.length + (send.files.length === 1 ? " file" : " files") + " · " + bytes(total) : "Send files to another device"),
        el("span", {}, send.files.length
          ? (tooMany ? "Up to " + S.send.maxFiles + " files and " + bytes(S.send.maxBytes) + " at once." : "Choose a device below.")
          : "Drop files here, or choose them. They go straight to your phone, another computer or a browser.")),
      button(send.busy ? "Adding…" : "Choose files", chooseFiles, { disabled: send.busy, icon: "plus" })),
    send.files.length ? el("div", { class: "files" }, send.files.map((file) =>
      el("div", { class: "file" },
        icon("file"),
        el("span", { class: "ellipsis", title: file.path }, file.name),
        el("span", { class: "muted" }, bytes(file.size)),
        iconButton("trash", "Remove " + file.name, () => removeFile(file))))) : null,
    deviceList(tooMany),
    el("div", { class: "xfers" }, S.send.transfers.map(transferRow)));
}

function deviceList(tooMany) {
  const devices = send.devices;
  if (send.deviceError) return el("div", { class: "devices none" }, send.deviceError);
  if (devices === null) return el("div", { class: "devices none" }, "Looking for your devices…");
  if (!devices.length) return el("div", { class: "devices none" }, "No other device is online right now. Open NeoCloud on your phone or in a browser.");
  return el("div", { class: "devices" }, devices.map((device) =>
    el("div", { class: "device" },
      dot("ok"), deviceIcon(device),
      el("span", { class: "ellipsis" }, device.name),
      button("Send", () => sendTo(device), { kind: "small primary", disabled: !send.files.length || tooMany || send.busy }))));
}

function transferRow(t) {
  const share = t.size > 0 ? Math.min(t.done / t.size, 1) : 1;
  const words = {
    uploading: ["busy", "Sending · " + Math.floor(share * 100) + "%"],
    waiting: ["busy", "Waiting for " + t.to + " to accept"],
    receiving: ["busy", t.to + " is receiving"],
    delivered: ["ok", "Delivered to " + t.to],
    declined: ["warn", t.to + " declined"],
    expired: ["warn", "Not received in time"],
    cancelled: ["", "Cancelled"],
    failed: ["bad", t.message || "Could not send"],
  };
  const [kind, word] = words[t.state] || ["", t.state];
  const bar = el("i", {});
  bar.style.width = Math.max(share * 100, 1) + "%";
  return el("div", { class: "xfer" },
    el("div", { class: "info" },
      el("div", { class: "local ellipsis" }, t.label || "Files", el("span", { class: "muted" }, " → " + t.to)),
      t.state === "uploading" ? el("div", { class: "meter" }, bar) : null,
      el("div", { class: "state" }, dot(kind), word)),
    t.active
      ? iconButton("back", "Cancel", () => act(() => call("DELETE", "/api/send/" + t.id)))
      : iconButton("check", "Dismiss", () => act(() => call("POST", "/api/send/" + t.id + "/dismiss"))));
}

setInterval(refreshSendDevices, 4000);
refreshSendDevices();
