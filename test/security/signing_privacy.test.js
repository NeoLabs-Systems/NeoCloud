'use strict';

// A signer never learns who the other parties are: no name, email or colour
// of another party appears in anything a link answers with (the final PDF's
// certificate pages are the audit record for everybody and are not a JSON
// answer). Their filled values stay visible for the preview.

const test = require('node:test');
const assert = require('node:assert/strict');
const { cleanup, registerUser } = require('../helpers/app');
const {
  area, consent, createDraft, makePng, publicGet, publicPost, publicPut, send, setUp, until, uploadImage,
} = require('../helpers/signing');

test.after(cleanup);

const PEOPLE = [
  { name: 'Zebulon Quarkfeather', email: 'zebulon.q@leak-check.test', color: '#112233' },
  { name: 'Ottoline Brightwater', email: 'ottoline.b@leak-check.test', color: '#445566' },
  { name: 'Cuthbert Moonshadow', email: 'cuthbert.m@leak-check.test', color: '#778899' },
];

// Everything that identifies one person to the others.
const secretsOf = (person) => [person.name, ...person.name.split(' '), person.email, person.email.split('@')[0], person.color];

function assertNoLeak(label, body, ...hidden) {
  const text = JSON.stringify(body);
  for (const person of hidden) {
    for (const secret of secretsOf(person)) {
      assert.ok(!text.toLowerCase().includes(secret.toLowerCase()), `${label} shows "${secret}"`);
    }
  }
}

async function threePartyDocument(agent) {
  const draft = await createDraft(agent);
  const { fields } = await setUp(agent, draft.document.id, {
    parties: PEOPLE,
    fields: [
      { party: 0, type: 'text', name: 'First text', areas: [area(0.1, 0.1)] },
      { party: 0, type: 'signature', name: 'First signature', areas: [area(0.1, 0.5)] },
      { party: 1, type: 'text', name: 'Second text', areas: [area(0.1, 0.2)] },
      { party: 1, type: 'signature', name: 'Second signature', areas: [area(0.1, 0.6)] },
      { party: 2, type: 'text', name: 'Third text', areas: [area(0.1, 0.3)] },
      { party: 2, type: 'signature', name: 'Third signature', areas: [area(0.1, 0.7)] },
    ],
  });
  const sent = await send(agent, draft.document.id);
  return { draft, fields, tokens: sent.tokens };
}

test('no link answer names, mails or colours another party, in any state', async () => {
  const { agent } = await registerUser('privacy');
  const png = await makePng();
  const { draft, fields, tokens } = await threePartyDocument(agent);
  const [first, second, third] = tokens;
  const [p0, p1, p2] = PEOPLE;

  // Waiting: the last one sees only counts.
  const waiting = (await publicGet(third).expect(200)).body;
  assert.equal(waiting.state, 'waiting');
  assert.deepEqual(waiting.progress, { done: 0, total: 3 });
  assert.deepEqual(waiting.turn, { canSign: false, waitingCount: 2 });
  assert.equal(waiting.parties, undefined);
  assertNoLeak('waiting page', waiting, p0, p1);
  assertNoLeak('waiting result', (await publicGet(third, '/result').expect(200)).body, p0, p1);

  // Ready: the first sees its own name and nobody else's.
  const ready = (await publicGet(first).expect(200)).body;
  assert.equal(ready.state, 'ready');
  assert.equal(ready.party.name, p0.name);
  assert.deepEqual(ready.progress, { done: 0, total: 3 });
  assertNoLeak('ready page', ready, p1, p2);
  assertNoLeak('consent answer', (await consent(first, 'Plain Signer')).body, p1, p2);
  assertNoLeak('saved value', (await publicPut(first, `/fields/${fields[0].id}`, { value: 'Filled in' }).expect(200)).body, p1, p2);
  await uploadImage(first, fields[1].id, png, 'method=type&text=Signed').expect(201);
  const completed = await publicPost(first, '/complete').expect(200);
  assertNoLeak('complete answer', completed.body, p1, p2);

  // Completed: the first, now done, still sees counts only.
  const done = (await publicGet(first).expect(200)).body;
  assert.equal(done.state, 'completed');
  assert.deepEqual(done.progress, { done: 1, total: 3 });
  assertNoLeak('completed page', done, p1, p2);

  // The second sees the first's filled values but not who filled them in.
  const turn = (await publicGet(second).expect(200)).body;
  assert.equal(turn.state, 'ready');
  assert.deepEqual(turn.progress, { done: 1, total: 3 });
  assert.equal(turn.turn.waitingCount, undefined);
  assert.ok(turn.fields.some((field) => !field.own && field.value === 'Filled in'));
  assertNoLeak('second ready page', turn, p0, p2);
  assertNoLeak('result of the second', (await publicGet(second, '/result').expect(200)).body, p0, p2);
  await publicGet(second, `/values/${fields[1].id}/image`).expect(200);

  // Everybody done: the third's last page and the owner-less results.
  await consent(second, 'Plain Signer');
  await publicPut(second, `/fields/${fields[2].id}`, { value: 'Second value' }).expect(200);
  await uploadImage(second, fields[3].id, png, 'method=type&text=Signed').expect(201);
  await publicPost(second, '/complete').expect(200);
  await consent(third, 'Plain Signer');
  await publicPut(third, `/fields/${fields[4].id}`, { value: 'Third value' }).expect(200);
  await uploadImage(third, fields[5].id, png, 'method=type&text=Signed').expect(201);
  await publicPost(third, '/complete').expect(200);
  await until(async () => (await agent.get(`/api/signing/documents/${draft.document.id}`).expect(200)).body.document.status === 'completed');
  for (const [token, hidden] of [[first, [p1, p2]], [second, [p0, p2]], [third, [p0, p1]]]) {
    const finished = (await publicGet(token).expect(200)).body;
    assert.deepEqual(finished.progress, { done: 3, total: 3 });
    assert.equal(finished.result.finalAvailable, true);
    assertNoLeak('finished page', finished, ...hidden);
    assertNoLeak('finished result', (await publicGet(token, '/result').expect(200)).body, ...hidden);
  }
});

test('a closed document shows the signer nothing about the others either', async () => {
  const { agent } = await registerUser('privacyclosed');
  const { draft, tokens } = await threePartyDocument(agent);
  await agent.post(`/api/signing/documents/${draft.document.id}/cancel`).send({}).expect(200);
  const closed = (await publicGet(tokens[1]).expect(200)).body;
  assert.equal(closed.state, 'canceled');
  assertNoLeak('closed page', closed, PEOPLE[0], PEOPLE[2]);
});
