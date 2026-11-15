'use strict';

// Which saved detail belongs in which field: names in English and German,
// accents and punctuation ignored, presets by label and synonym.

const test = require('node:test');
const assert = require('node:assert/strict');
const { fillKind, matchName, normalizeName, valueOfSource } = require('../../server/services/signing/prefill_match');

const DETAILS = {
  displayName: 'Maria Anna Müller', email: 'maria@example.org', phone: '+49 30 1234',
  presets: [
    { label: 'Adresse', value: 'Hauptstr. 1, Berlin' },
    { label: 'Firma', value: 'ACME GmbH' },
    { label: 'Title', value: 'Dr.' },
    { label: 'Hobby', value: 'Chess' },
  ],
};

const fill = (fieldName) => {
  const source = matchName(fieldName, DETAILS.presets);
  return source ? valueOfSource(source, DETAILS) : null;
};

test('names are normalized: case, accents, ß and punctuation do not matter', () => {
  assert.equal(normalizeName('  E-Mail  Adresse: '), 'e mail adresse');
  assert.equal(normalizeName('Vollständiger NAME'), 'vollstandiger name');
  assert.equal(normalizeName('Straße'), 'strasse');
  assert.equal(normalizeName(null), '');
});

test('the matching table, English and German', () => {
  const table = [
    ['Full name', 'Maria Anna Müller'],
    ['FULL NAME', 'Maria Anna Müller'],
    ['Name', 'Maria Anna Müller'],
    ['Vor- und Nachname', 'Maria Anna Müller'],
    ['Vorname', 'Maria Anna'],
    ['First name', 'Maria Anna'],
    ['Nachname', 'Müller'],
    ['Last name', 'Müller'],
    ['Surname', 'Müller'],
    ['Email', 'maria@example.org'],
    ['E-Mail', 'maria@example.org'],
    ['E-Mail-Adresse', 'maria@example.org'],
    ['Your email address', 'maria@example.org'],
    ['Phone', '+49 30 1234'],
    ['Telefon', '+49 30 1234'],
    ['Telefonnummer', '+49 30 1234'],
    ['Mobil', '+49 30 1234'],
    ['Handy', '+49 30 1234'],
    ['Mobile number', '+49 30 1234'],
    ['Adresse', 'Hauptstr. 1, Berlin'],
    ['Address', 'Hauptstr. 1, Berlin'],
    ['Anschrift', 'Hauptstr. 1, Berlin'],
    ['Straße', 'Hauptstr. 1, Berlin'],
    ['Street address', 'Hauptstr. 1, Berlin'],
    ['Firma', 'ACME GmbH'],
    ['Company', 'ACME GmbH'],
    ['Company name', 'ACME GmbH'],
    ['Arbeitgeber', 'ACME GmbH'],
    ['Titel', 'Dr.'],
    ['Job title', 'Dr.'],
    ['Hobby', 'Chess'],
  ];
  for (const [field, expected] of table) assert.equal(fill(field), expected, field);
});

test('what is not clearly a known detail is left alone', () => {
  for (const field of ['Tenant name', 'Name of the bank', 'Account number', 'Ort, Datum', 'Signature place', '', '   ', 'Comments', 'Mail merge field']) {
    assert.equal(fill(field), null, field);
  }
});

test('a longer, more specific phrase beats a shorter one; a preset beats a builtin on a tie', () => {
  // "e mail adresse" is an email, not an address, although it contains "adresse".
  assert.equal(fill('E-Mail Adresse'), 'maria@example.org');
  const own = [{ label: 'Name', value: 'Preset name' }];
  assert.equal(valueOfSource(matchName('Name', own), DETAILS), 'Preset name');
});

test('names with one part and empty details', () => {
  const single = { ...DETAILS, displayName: 'Cher' };
  assert.equal(valueOfSource({ key: 'firstName' }, single), 'Cher');
  assert.equal(valueOfSource({ key: 'lastName' }, single), '');
  assert.equal(valueOfSource({ key: 'email' }, { ...DETAILS, email: '' }), '');
});

test('what kind of fill a field gets', () => {
  assert.equal(fillKind({ type: 'signature', prefs: {} }), 'image');
  assert.equal(fillKind({ type: 'initials', prefs: {} }), 'image');
  assert.equal(fillKind({ type: 'date', prefs: { autoToday: true } }), 'today');
  assert.equal(fillKind({ type: 'date', prefs: {} }), null);
  assert.equal(fillKind({ type: 'text', prefs: {} }), 'named');
  assert.equal(fillKind({ type: 'phone', prefs: {} }), 'named');
  for (const type of ['checkbox', 'file', 'image', 'stamp', 'heading', 'multiple', 'cells']) assert.equal(fillKind({ type, prefs: {} }), null, type);
});
