'use strict';

// Who is acting while a tree operation runs. File services work as the
// owner of the tree (a member's write lands in the owner's account), so the
// member layer states the real actor here and the activity log reads it.

const { AsyncLocalStorage } = require('async_hooks');

const store = new AsyncLocalStorage();

/**
 * `fn(userId, ...args)` as the member layer's mutations are called: the
 * caller is the actor even when the tree being changed is another account's.
 */
function actingAsCaller(fn) {
  return (userId, ...args) => store.run(userId, () => fn(userId, ...args));
}

/** The acting account, or null outside runAs (then the owner acts). */
function currentActor() {
  return store.getStore() ?? null;
}

module.exports = { actingAsCaller, currentActor };
