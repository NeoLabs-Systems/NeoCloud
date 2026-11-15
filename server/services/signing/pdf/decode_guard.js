'use strict';

// pdf-lib inflates object and xref streams while it loads a file, and one
// deflate stream can expand about 1000:1: a 1.5 MB file made the server
// allocate 3.8 GB and stand still for 4 seconds. Real object streams are a
// few megabytes at most, so no stream may inflate past MAX_DECODED_BYTES;
// the load then fails like any other broken PDF (PDF_INVALID).

const DecodeStream = require('pdf-lib/cjs/core/streams/DecodeStream').default;
const { MAX_DECODED_STREAM_BYTES } = require('./limits');

const ensureBuffer = DecodeStream.prototype.ensureBuffer;

if (!ensureBuffer.guarded) {
  DecodeStream.prototype.ensureBuffer = function guardedEnsureBuffer(requested) {
    if (requested > MAX_DECODED_STREAM_BYTES) throw new Error('A PDF stream expands past the allowed size.');
    return ensureBuffer.call(this, requested);
  };
  DecodeStream.prototype.ensureBuffer.guarded = true;
}
