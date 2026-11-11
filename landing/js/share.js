'use strict';

// The public page of a NeoCloud share link (/s/:token): shows the shared
// file, or browses the shared folder, downloads, and uploads when allowed.
// An upload-only link shows just a drop zone and a receipt. Everything from
// the server goes into the page as text, never as markup. Dates are shown in
// the visitor's own time zone.
//
// Uploads are one PUT per file: the public upload route has no resumable
// parts (the owner-side /api/uploads has), so a dropped connection restarts
// that file.

(function sharePage() {
  const token = decodeURIComponent(location.pathname.split('/')[2] || '');
  const api = `/api/public/shares/${encodeURIComponent(token)}`;
  const $ = (selector) => document.querySelector(selector);

  const FOLDER_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>';
  const FILE_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/></svg>';

  let currentFolderId = '';
  let received = 0;

  function formatBytes(bytes) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let value = Number(bytes) || 0;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit += 1;
    }
    return `${unit === 0 ? value : value.toFixed(1)} ${units[unit]}`;
  }

  function show(section) {
    for (const name of ['lock', 'file', 'folder']) $(`[data-${name}]`).hidden = name !== section;
  }

  function fail(title, detail) {
    show(null);
    $('[data-eyebrow]').textContent = 'Share link';
    $('[data-title]').textContent = title;
    $('[data-meta]').textContent = detail || '';
  }

  function contentUrl(id) {
    return `${api}/files/${encodeURIComponent(id)}/content?download=1`;
  }

  function zipUrl(folderId) {
    return folderId ? `${api}/zip?folderId=${encodeURIComponent(folderId)}` : `${api}/zip`;
  }

  // Server times are UTC ("2026-11-11 09:30:00"); shown in the local zone.
  function localTime(value) {
    const date = new Date(`${value.replace(' ', 'T')}Z`);
    return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date);
  }

  // "Shared by alice from cloud.example.com · expires 12 Nov 2026, 10:30"
  function sharerText(share) {
    const parts = [];
    if (share.sharedBy) parts.push(`Shared by ${share.sharedBy}${share.host ? ` from ${share.host}` : ''}`);
    if (share.expiresAt) parts.push(`expires ${localTime(share.expiresAt)}`);
    return parts.join(' · ');
  }

  function renderShare(share, detail) {
    $('[data-meta]').textContent = [sharerText(share), detail].filter(Boolean).join(' · ');
    const note = $('[data-note]');
    note.textContent = share.note || '';
    note.hidden = !share.note;
  }

  function renderFile(data) {
    show('file');
    $('[data-title]').textContent = data.node.name;
    renderShare(data.share, formatBytes(data.node.size));
    $('[data-file-name]').textContent = data.node.name;
    $('[data-file-size]').textContent = formatBytes(data.node.size);
    $('[data-file-download]').href = contentUrl(data.node.id);
  }

  function renderCrumbs(path) {
    const crumbs = $('[data-crumbs]');
    crumbs.replaceChildren();
    path.forEach((part, index) => {
      if (index > 0) {
        const slash = document.createElement('span');
        slash.textContent = '/';
        crumbs.append(slash);
      }
      if (index === path.length - 1) {
        const current = document.createElement('b');
        current.textContent = part.name;
        crumbs.append(current);
        return;
      }
      const link = document.createElement('button');
      link.type = 'button';
      link.textContent = part.name;
      link.addEventListener('click', () => load(index === 0 ? '' : part.id));
      crumbs.append(link);
    });
  }

  function renderRow(item) {
    const row = document.createElement('li');
    const icon = document.createElement('span');
    icon.className = 'icon';
    icon.innerHTML = item.type === 'folder' ? FOLDER_ICON : FILE_ICON;
    let name;
    if (item.type === 'folder') {
      name = document.createElement('button');
      name.type = 'button';
      name.addEventListener('click', () => load(item.id));
    } else {
      name = document.createElement('a');
      name.href = contentUrl(item.id);
    }
    name.className = 'name';
    name.textContent = item.name;
    const size = document.createElement('span');
    size.className = 'size';
    size.textContent = item.type === 'folder' ? '' : formatBytes(item.size);
    row.append(icon, name, size);
    return row;
  }

  // Upload-only: no listing, just the drop zone and what arrived.
  function renderInbox(data) {
    show('folder');
    $('[data-eyebrow]').textContent = 'Send files';
    $('[data-title]').textContent = data.node.name;
    renderShare(data.share, '');
    $('[data-folder-head]').hidden = true;
    $('[data-rows]').hidden = true;
    $('[data-drop-text]').textContent = data.share.sharedBy
      ? `Drop files here to send them to ${data.share.sharedBy}. You won’t see what is already in the folder.`
      : 'Drop files here to send them. You won’t see what is already in the folder.';
    $('[data-drop]').hidden = false;
  }

  function renderFolder(data) {
    show('folder');
    $('[data-folder-head]').hidden = false;
    $('[data-rows]').hidden = false;
    const root = data.path[0];
    $('[data-title]').textContent = root ? root.name : data.node.name;
    renderShare(data.share, `${data.items.length} item${data.items.length === 1 ? '' : 's'}`);
    renderCrumbs(data.path);
    $('[data-zip]').href = zipUrl(currentFolderId);
    const rows = $('[data-rows]');
    if (data.items.length === 0) {
      const empty = document.createElement('li');
      empty.className = 'empty';
      empty.textContent = 'This folder is empty.';
      rows.replaceChildren(empty);
    } else {
      rows.replaceChildren(...data.items.map(renderRow));
    }
    $('[data-drop]').hidden = !data.share.allowUpload;
  }

  async function load(folderId) {
    currentFolderId = folderId || '';
    const query = currentFolderId ? `?folderId=${encodeURIComponent(currentFolderId)}` : '';
    let res;
    try {
      res = await fetch(`${api}${query}`, { credentials: 'same-origin', headers: { Accept: 'application/json' } });
    } catch {
      fail('The server can’t be reached.', 'Check your connection and reload the page.');
      return;
    }
    const data = await res.json().catch(() => ({}));
    if (res.ok) {
      if (data.share.mode === 'upload_only') renderInbox(data);
      else if (data.share.type === 'file') renderFile(data);
      else renderFolder(data);
      return;
    }
    if (res.status === 401 && data.code === 'SHARE_PASSWORD_REQUIRED') {
      show('lock');
      $('[data-title]').textContent = 'Protected link';
      $('[data-meta]').textContent = '';
      $('#share-password').focus();
      return;
    }
    if (res.status === 410) {
      const titles = {
        SHARE_EXPIRED: 'This link has expired.',
        SHARE_EXHAUSTED: 'This link has reached its download limit.',
      };
      fail(titles[data.code] || 'This link has been turned off.', 'Ask the person who shared it for a new one.');
      return;
    }
    if (res.status === 429) {
      fail('Too many requests.', 'Wait a few minutes and try again.');
      return;
    }
    fail('This link doesn’t exist.', 'It may have been mistyped, or the shared item was deleted.');
  }

  async function unlock(event) {
    event.preventDefault();
    const error = $('[data-lock-error]');
    error.textContent = '';
    const res = await fetch(`${api}/unlock`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: $('#share-password').value }),
    }).catch(() => null);
    if (res && res.ok) {
      $('#share-password').value = '';
      load('');
      return;
    }
    const data = res ? await res.json().catch(() => ({})) : {};
    if (data.code === 'SHARE_PASSWORD_INVALID') error.textContent = 'Wrong password.';
    else if (data.code === 'SHARE_LOCKED') error.textContent = `Too many wrong passwords. Try again in ${waitText(data.retryAfter)}.`;
    else error.textContent = data.error || 'Could not open the link.';
  }

  function waitText(seconds) {
    const value = Number(seconds) || 60;
    if (value < 90) return `${value} second${value === 1 ? '' : 's'}`;
    return `${Math.ceil(value / 60)} minutes`;
  }

  async function uploadFiles(files) {
    const list = $('[data-uploads]');
    for (const file of files) {
      const line = document.createElement('li');
      line.textContent = `${file.name} · uploading…`;
      list.append(line);
      const query = new URLSearchParams({ name: file.name });
      if (currentFolderId) query.set('folderId', currentFolderId);
      const res = await fetch(`${api}/upload?${query}`, {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/octet-stream' },
        body: file,
      }).catch(() => null);
      if (res && res.ok) {
        const node = await res.json();
        line.className = 'ok';
        line.textContent = `${node.name} · ${formatBytes(node.size)} · uploaded`;
        received += 1;
        const receipt = $('[data-receipt]');
        receipt.textContent = `${received} file${received === 1 ? '' : 's'} received`;
        receipt.hidden = false;
      } else {
        const data = res ? await res.json().catch(() => ({})) : {};
        line.className = 'failed';
        line.textContent = `${file.name} · ${data.error || 'upload failed'}`;
      }
    }
    load(currentFolderId);
  }

  function wireUploads() {
    const drop = $('[data-drop]');
    const picker = $('[data-picker]');
    $('[data-pick]').addEventListener('click', () => picker.click());
    picker.addEventListener('change', () => {
      uploadFiles([...picker.files]);
      picker.value = '';
    });
    drop.addEventListener('dragover', (event) => {
      event.preventDefault();
      drop.classList.add('over');
    });
    drop.addEventListener('dragleave', () => drop.classList.remove('over'));
    drop.addEventListener('drop', (event) => {
      event.preventDefault();
      drop.classList.remove('over');
      uploadFiles([...event.dataTransfer.files]);
    });
  }

  $('[data-lock-form]').addEventListener('submit', unlock);
  wireUploads();
  if (!token) fail('This link doesn’t exist.', '');
  else load('');
}());
