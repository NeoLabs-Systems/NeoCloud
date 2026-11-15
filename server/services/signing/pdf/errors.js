'use strict';

/**
 * Typed failure of the PDF engine. `code` is part of the contract with the
 * signing API (which maps it to an HTTP status):
 *
 *   PDF_INVALID              not a readable PDF
 *   PDF_ENCRYPTED            password/permission protected; not supported
 *   PDF_TOO_LARGE            over the byte cap
 *   PDF_TOO_MANY_PAGES       over the page cap
 *   IMAGE_INVALID            not a usable image / unsupported image format
 *   CONVERSION_UNAVAILABLE   no LibreOffice on this host
 *   CONVERSION_FAILED        LibreOffice ran but produced nothing usable
 *   UNSUPPORTED_DOCUMENT     extension we do not hand to LibreOffice
 *   HASH_MISMATCH            the snapshot does not match its recorded SHA-256
 *   SIGNING_FAILED           bad P12/passphrase or the signature did not fit
 */
class PdfError extends Error {
  constructor(code, message, options) {
    super(message || code, options);
    this.name = 'PdfError';
    this.code = code;
  }
}

module.exports = { PdfError };
