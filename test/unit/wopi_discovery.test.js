'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { editorPageUrl, parseDiscovery } = require('../../server/utils/wopi_discovery');

const DISCOVERY = `<?xml version="1.0" encoding="utf-8"?>
<wopi-discovery>
  <net-zone name="external-http">
    <app name="writer">
      <action name="edit" ext="docx" urlsrc="https://office.example.com/browser/abc/cool.html?"/>
      <action name="edit" ext="odt" default="true" urlsrc="https://office.example.com/browser/abc/cool.html?"/>
      <action name="view" ext="docx" urlsrc="https://office.example.com/browser/abc/cool.html?view=1&amp;"/>
    </app>
    <app name="calc">
      <action name="edit" ext="XLSX" urlsrc="https://office.example.com/browser/abc/cool.html?"/>
      <action name="edit" ext="__proto__" urlsrc="https://office.example.com/x"/>
      <action name="edit" ext="ods" urlsrc="javascript:alert(1)"/>
      <action name="convert-to" ext="pdf" urlsrc="https://office.example.com/convert"/>
    </app>
    <app name="image"><action name="view" ext="png" urlsrc="https://office.example.com/img?&lt;ui=UI_LLCC&amp;&gt;"/></app>
  </net-zone>
</wopi-discovery>`;

test('maps each extension to its edit and view pages', () => {
  const actions = parseDiscovery(DISCOVERY);
  assert.equal(actions.docx.edit, 'https://office.example.com/browser/abc/cool.html?');
  assert.equal(actions.docx.view, 'https://office.example.com/browser/abc/cool.html?view=1&');
  assert.equal(actions.odt.edit, 'https://office.example.com/browser/abc/cool.html?');
  assert.ok(actions.xlsx, 'extensions are lower-cased');
});

test('ignores other actions, unsafe addresses and odd extensions', () => {
  const actions = parseDiscovery(DISCOVERY);
  assert.equal(actions.pdf, undefined);
  assert.equal(actions.ods, undefined);
  assert.equal(Object.getPrototypeOf(actions), null);
  assert.deepEqual(Object.keys(actions).sort(), ['docx', 'odt', 'png', 'xlsx']);
});

test('refuses text that is not a discovery document', () => {
  assert.throws(() => parseDiscovery('<html>nginx</html>'), /not a WOPI discovery/);
  assert.throws(() => parseDiscovery(''), /not a WOPI discovery/);
});

test('builds the editor page URL with WOPISrc', () => {
  const src = 'https://host/files/1';
  const encoded = encodeURIComponent(src);
  assert.equal(editorPageUrl('https://o/cool.html?', src), `https://o/cool.html?WOPISrc=${encoded}`);
  assert.equal(editorPageUrl('https://o/cool.html?a=1&', src), `https://o/cool.html?a=1&WOPISrc=${encoded}`);
  assert.equal(editorPageUrl('https://o/cool.html?a=1', src), `https://o/cool.html?a=1&WOPISrc=${encoded}`);
  assert.equal(editorPageUrl('https://o/cool.html', src), `https://o/cool.html?WOPISrc=${encoded}`);
  assert.equal(editorPageUrl('https://o/e?<ui=UI_LLCC&>', src), `https://o/e?WOPISrc=${encoded}`);
});
