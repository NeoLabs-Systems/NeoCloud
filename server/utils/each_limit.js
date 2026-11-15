'use strict';

/** Calls worker(item) for every item, at most `limit` at a time; rejects on the first failure. */
async function eachLimit(items, limit, worker) {
  let next = 0;
  async function lane() {
    while (next < items.length) {
      const item = items[next];
      next += 1;
      await worker(item);
    }
  }
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, lane));
}

module.exports = { eachLimit };
