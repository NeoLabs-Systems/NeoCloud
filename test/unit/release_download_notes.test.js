'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { NOTES_END, NOTES_START, buildDownloadNotes, classifyAppAsset, mergeDownloadNotes } = require('../../scripts/release_download_notes');

const TAG = 'v0.1.1-beta.33';
const ASSETS = [
  'NeoCloud-v0.1.1-beta.33-macos.dmg',
  'NeoCloud-Setup-v0.1.1-beta.33-windows-x64.exe',
  'NeoCloud-Setup-v0.1.1-beta.33-windows-arm64.exe',
  'NeoCloud-v0.1.1-beta.33-linux-x86_64.AppImage',
  'neocloud-v0.1.1-beta.33-android.apk',
  'neocloud-lite-v0.1.1-beta.33-linux-amd64',
  'SHA256SUMS.txt',
].map((name) => ({ name }));

test('each release file finds its button; raw binaries and checksums get none', () => {
  assert.deepEqual(ASSETS.map((asset) => classifyAppAsset(asset.name)?.id ?? null), [
    'macos', 'windows-x64', 'windows-arm64', 'linux-appimage', 'android', null, null,
  ]);
});

test('ready apps link to their file, missing ones say building', () => {
  const partial = buildDownloadNotes({ tag: TAG, assets: ASSETS.slice(0, 1) });
  assert.match(partial, /href="https:\/\/github\.com\/NeoLabs-Systems\/NeoCloud\/releases\/download\/v0\.1\.1-beta\.33\/NeoCloud-v0\.1\.1-beta\.33-macos\.dmg"/);
  assert.match(partial, /Windows-building-6b7280/);
  assert.match(partial, /The rest attach/);
  const full = buildDownloadNotes({ tag: TAG, assets: ASSETS });
  assert.doesNotMatch(full, /building/);
  assert.match(full, /Every desktop and Android app for this release is ready\./);
  assert.match(full, /docker pull ghcr\.io\/neolabs-systems\/neocloud:0\.1\.1-beta\.33/);
});

test('a refresh replaces only the download section and keeps the changelog', () => {
  const first = mergeDownloadNotes('**Full Changelog**: x...y', buildDownloadNotes({ tag: TAG }));
  assert.ok(first.startsWith(NOTES_START));
  assert.match(first, /\*\*Full Changelog\*\*: x\.\.\.y\n?$/);
  const second = mergeDownloadNotes(first, buildDownloadNotes({ tag: TAG, assets: ASSETS }));
  assert.equal(second.split(NOTES_START).length, 2);
  assert.equal(second.split(NOTES_END).length, 2);
  assert.match(second, /Full Changelog/);
  assert.doesNotMatch(second, /building/);
});
