'use strict';

// Email addresses as the audit trail and error texts may show them: a
// masked form, never the address itself ("a***@example.com").

const ADDRESS = /[^\s@<>,;:"()[\]\\]+@[^\s@<>,;:"()[\]\\]+\.[^\s@<>,;:"()[\]\\]+/g;

function maskEmail(address) {
  const text = String(address || '');
  const at = text.lastIndexOf('@');
  if (at < 1) return '***';
  return `${text.slice(0, 1)}***${text.slice(at)}`;
}

/** The text with every address in it masked. */
function maskAddressesIn(text) {
  return String(text || '').replace(ADDRESS, (address) => maskEmail(address));
}

module.exports = { maskAddressesIn, maskEmail };
