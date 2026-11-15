'use strict';

// Cuts bytes start..end out of a stream whose source ignored the Range request.

const { Transform } = require('stream');

function sliceStream({ start, end }) {
  let seen = 0;
  return new Transform({
    transform(chunk, encoding, callback) {
      const from = Math.max(start - seen, 0);
      const to = Math.min(end + 1 - seen, chunk.length);
      seen += chunk.length;
      if (to > from) this.push(chunk.subarray(from, to));
      if (seen > end) this.push(null);
      callback();
    },
  });
}

module.exports = { sliceStream };
