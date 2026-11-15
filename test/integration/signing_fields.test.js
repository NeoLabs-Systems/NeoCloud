'use strict';

// Field validation: the editor's field list, and every field type's values.

const test = require('node:test');
const assert = require('node:assert/strict');
const { cleanup, registerUser } = require('../helpers/app');
const {
  area, consent, createDraft, makePng, publicGet, publicPost, publicPut, send, setUp, uploadImage,
} = require('../helpers/signing');

test.after(cleanup);

const OPTIONS = ['Red', 'Green', 'Blue'];
const TYPES = [
  ['text', { maxLength: 5 }],
  ['number', { min: 0, max: 100, decimals: 1 }],
  ['date', {}],
  ['phone', {}],
  ['checkbox', {}],
  ['radio', { options: OPTIONS }],
  ['select', { options: OPTIONS }],
  ['multiple', { options: OPTIONS, maxSelect: 2 }],
  ['cells', { cells: 4, charset: 'digits' }],
];

async function documentWithAllTypes() {
  const { agent } = await registerUser('types');
  const draft = await createDraft(agent);
  const fields = TYPES.map(([type, prefs], index) => ({ type, prefs, name: type, areas: [area(0.1, 0.05 + index * 0.08)] }));
  const stored = await setUp(agent, draft.document.id, { parties: [{ name: 'A' }], fields });
  const { tokens } = await send(agent, draft.document.id);
  await consent(tokens[0]);
  const ids = Object.fromEntries(stored.fields.map((field) => [field.type, field.id]));
  return { agent, draft, ids, token: tokens[0] };
}

test('every typed field accepts good values and refuses bad ones', async () => {
  const { ids, token } = await documentWithAllTypes();
  const good = {
    text: 'Hello', number: '12.5', date: '2026-02-28', phone: '+49 170 1234567', checkbox: true,
    radio: 'Green', select: 'Blue', multiple: ['Blue', 'Red'], cells: '1234',
  };
  for (const [type, value] of Object.entries(good)) {
    const res = await publicPut(token, `/fields/${ids[type]}`, { value }).expect(200);
    assert.deepEqual(res.body.value, type === 'multiple' ? ['Red', 'Blue'] : value, type);
  }
  const bad = {
    text: 'too long for this', number: '12.55', date: '2026-02-30', phone: 'call me', checkbox: 'yes',
    radio: 'Purple', select: 5, multiple: ['Red', 'Green', 'Blue'], cells: '12a4',
  };
  for (const [type, value] of Object.entries(bad)) {
    const res = await publicPut(token, `/fields/${ids[type]}`, { value });
    assert.equal(res.status, 400, type);
    assert.equal(res.body.code, 'INVALID_VALUE', type);
  }
  await publicPut(token, `/fields/${ids.number}`, { value: '101' }).expect(400);
  await publicPut(token, `/fields/${ids.number}`, { value: '1e3' }).expect(400);
  // Nothing bad was stored, and null clears.
  assert.equal((await publicGet(token).expect(200)).body.myValues[ids.text], 'Hello');
  await publicPut(token, `/fields/${ids.text}`, { value: null }).expect(200);
  assert.equal((await publicGet(token).expect(200)).body.myValues[ids.text], undefined);
  await publicPut(token, `/fields/${ids.text}`, {}).expect(400);
  // A typed value cannot go into an upload field and vice versa.
  const png = await makePng();
  await uploadImage(token, ids.text, png).expect(400);
});

test('the editor refuses invalid fields and keeps ids stable', async () => {
  const { agent } = await registerUser('editor');
  const draft = await createDraft(agent);
  const { parties } = await setUp(agent, draft.document.id, { parties: [{ name: 'A' }, { name: 'B' }], fields: [] });
  const url = `/api/signing/documents/${draft.document.id}/fields`;
  const field = { partyId: parties[0].id, type: 'text', areas: [area()] };
  const refused = async (patch, label) => {
    const res = await agent.put(url).send({ fields: [{ ...field, ...patch }] });
    assert.equal(res.status, 400, label);
    assert.equal(res.body.code, 'INVALID_FIELD', label);
  };
  await refused({ type: 'payment' }, 'unknown type');
  await refused({ partyId: 'nope' }, 'unknown party');
  await refused({ areas: [] }, 'no area');
  await refused({ areas: [area(0.9, 0.1, 0.3)] }, 'outside the page');
  await refused({ areas: [area(0.1, 0.1, 0.3, 0.05, 2)] }, 'page does not exist');
  await refused({ areas: [{ page: 0, x: 'a', y: 0, w: 0.1, h: 0.1 }] }, 'not numbers');
  await refused({ prefs: { fontSize: 500 } }, 'font size');
  await refused({ prefs: { surprise: true } }, 'unknown pref');
  await refused({ type: 'radio' }, 'options needed');
  await refused({ type: 'signature', readonly: true }, 'read-only signature');
  await refused({ type: 'signature', defaultValue: 'x' }, 'default on upload');
  await refused({ type: 'date', defaultValue: '31.12.2026' }, 'default must be valid');
  await refused({ name: 'x'.repeat(101) }, 'long name');
  await agent.put(url).send({ fields: 'nope' }).expect(400);
  const tooMany = Array.from({ length: 501 }, () => field);
  await agent.put(url).send({ fields: tooMany }).expect(400);

  const first = await agent.put(url).send({ fields: [
    { ...field, readonly: true, defaultValue: 'Fixed', required: true },
    { ...field, partyId: parties[1].id, type: 'cells', prefs: { cells: 6 }, areas: [area(), area(0.1, 0.5, 0.3, 0.05, 1)] },
  ] }).expect(200);
  assert.equal(first.body.fields[0].required, false, 'read-only fields cannot be required');
  assert.equal(first.body.fields[1].areas.length, 2);
  const second = await agent.put(url).send({ fields: [{ ...first.body.fields[1], name: 'Renamed' }] }).expect(200);
  assert.equal(second.body.fields.length, 1);
  assert.equal(second.body.fields[0].id, first.body.fields[1].id);
  await agent.put(url).send({ fields: [first.body.fields[1], first.body.fields[1]] }).expect(400);

  await agent.put(`/api/signing/documents/${draft.document.id}/parties`).send({ parties: [] }).expect(400);
  await agent.put(`/api/signing/documents/${draft.document.id}/parties`).send({ parties: [{ name: ' ' }] }).expect(400);
  await agent.put(`/api/signing/documents/${draft.document.id}/parties`).send({ parties: [{ name: 'A', email: 'nope' }] }).expect(400);
  await agent.put(`/api/signing/documents/${draft.document.id}/parties`).send({ parties: [{ name: 'A', color: 'red' }] }).expect(400);
  const kept = await agent.put(`/api/signing/documents/${draft.document.id}/parties`)
    .send({ parties: [{ id: parties[1].id, name: 'Only B', email: 'b@example.com' }] }).expect(200);
  assert.equal(kept.body.parties[0].id, parties[1].id);
  assert.equal(kept.body.fields.length, 1, 'the fields of a removed party go with it');
  assert.equal(kept.body.fields[0].partyId, parties[1].id);
  const expiry = await agent.put(`/api/signing/documents/${draft.document.id}`).send({ expiresAt: '2001-01-01T00:00:00Z' }).expect(400);
  assert.equal(expiry.body.code, 'EXPIRY_IN_PAST');
  await agent.put(`/api/signing/documents/${draft.document.id}`).send({ signingOrder: 'sideways' }).expect(400);
  await agent.put(`/api/signing/documents/${draft.document.id}`).send({ locale: 'xx' }).expect(400);
});

test('defaults are kept as values, read-only fields are not writable, other parties cannot be written', async () => {
  const { agent } = await registerUser('defaults');
  const draft = await createDraft(agent);
  const stored = await setUp(agent, draft.document.id, {
    parties: [{ name: 'A' }, { name: 'B' }],
    fields: [
      { party: 0, type: 'text', required: true, defaultValue: 'Prefilled', areas: [area()] },
      { party: 0, type: 'text', readonly: true, defaultValue: 'Fixed', areas: [area(0.1, 0.3)] },
      { party: 1, type: 'text', areas: [area(0.1, 0.5)] },
      { party: 0, type: 'heading', defaultValue: 'Section', areas: [area(0.1, 0.7)] },
    ],
  });
  const { tokens } = await send(agent, draft.document.id);
  const [a] = tokens;
  await consent(a);
  const [prefilled, fixed, others, heading] = stored.fields;
  await publicPut(a, `/fields/${fixed.id}`, { value: 'hack' }).expect(404);
  await publicPut(a, `/fields/${heading.id}`, { value: 'hack' }).expect(404);
  await publicPut(a, `/fields/${others.id}`, { value: 'hack' }).expect(404);
  const page = await publicGet(a).expect(200);
  assert.equal(page.body.myValues[prefilled.id], 'Prefilled');
  assert.equal(page.body.myValues[fixed.id], 'Fixed');
  assert.ok(page.body.fields.some((field) => field.id === heading.id));
  await publicPost(a, '/complete').expect(200);
  const record = await agent.get(`/api/signing/documents/${draft.document.id}/preview-values`).expect(200);
  assert.equal(record.body.values[prefilled.id], 'Prefilled', 'the untouched default was stored');
  assert.equal(record.body.values[fixed.id], 'Fixed');
});
