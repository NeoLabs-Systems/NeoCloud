'use strict';

// Notes (docs/notes.md): Markdown files under /Notes shown as cards, with
// AI help on demand. A browser session or the phone app; desktop tokens
// reach the same files through the file routes instead.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const store = require('../services/notes/store');
const { assistNote } = require('../services/notes/ai');
const { jsonHandler, rateLimiter } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);

// Autosave writes often; model calls cost tokens.
const aiLimiter = rateLimiter(120);

const text = (value) => (typeof value === 'string' ? value : undefined);

router.get('/', jsonHandler((req) => store.listNotes(req.userId, {
  q: text(req.query.q),
  tag: text(req.query.tag),
  notebook: text(req.query.notebook),
  archived: text(req.query.archived),
})));
router.post('/', jsonHandler((req) => store.createNote(req.userId, req.body || {}), { status: 201 }));
router.post('/root', jsonHandler((req) => store.ensureRoot(req.userId)));
router.post('/notebooks', jsonHandler((req) => store.createNotebook(req.userId, req.body || {})));
router.get('/:id', jsonHandler((req) => store.getNote(req.userId, req.params.id)));
router.patch('/:id', jsonHandler((req) => store.updateNote(req.userId, req.params.id, req.body || {})));
router.delete('/:id', jsonHandler((req) => store.deleteNote(req.userId, req.params.id)));
router.post('/:id/ai', aiLimiter, jsonHandler((req) => assistNote(req.userId, req.params.id, req.body || {})));

module.exports = router;
