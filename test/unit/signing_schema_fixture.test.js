'use strict';

// The Flutter registry test reads flutter_app/test/fixtures/signing_server_schema.json.
// This keeps that file in step with services/signing/field_types.js.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const { FIXTURE, buildSchema } = require('../../scripts/dump_signing_schema');

test('the committed signing schema matches the server (run node scripts/dump_signing_schema.js)', () => {
  assert.deepEqual(JSON.parse(fs.readFileSync(FIXTURE, 'utf8')), JSON.parse(JSON.stringify(buildSchema())));
});
