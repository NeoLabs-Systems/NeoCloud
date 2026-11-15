'use strict';

// Writes the server's field-type schema (types, storage, accepted prefs and
// their limits) to the JSON file the Flutter registry test compares against.
// Run after changing services/signing/field_types.js:
//   node scripts/dump_signing_schema.js
// test/unit/signing_schema_fixture.test.js fails while the file is stale.

const fs = require('fs');
const path = require('path');
const { TYPES } = require('../server/services/signing/field_types');

const FIXTURE = path.join(__dirname, '..', 'flutter_app', 'test', 'fixtures', 'signing_server_schema.json');

function describePref(spec) {
  const entry = { kind: spec.kind };
  for (const key of ['min', 'max', 'maxItems', 'maxLength', 'values']) {
    if (spec[key] !== undefined) entry[key] = spec[key];
  }
  if (spec.regex) entry.regex = spec.regex.source;
  return entry;
}

function buildSchema() {
  const types = {};
  for (const name of Object.keys(TYPES).sort()) {
    const type = TYPES[name];
    const prefs = {};
    for (const key of Object.keys(type.prefs).sort()) prefs[key] = describePref(type.prefs[key]);
    types[name] = { storage: type.storage, requiredPrefs: type.requiredPrefs || [], prefs };
  }
  return { types };
}

if (require.main === module) {
  fs.mkdirSync(path.dirname(FIXTURE), { recursive: true });
  fs.writeFileSync(FIXTURE, `${JSON.stringify(buildSchema(), null, 2)}\n`);
}

module.exports = { FIXTURE, buildSchema };
