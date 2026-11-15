'use strict';

// How a signing link reaches a signer. Today the owner copies the link and
// sends it themselves ("link" channel). Email, when it comes, is another
// channel here: it receives the same context and reports whether it sent.

const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Signing');

const linkOnly = {
  channel: 'link',
  async deliver() {
    return { delivered: false, channel: 'link' };
  },
};

const provider = linkOnly;

/**
 * Hands a fresh link to the delivery channel.
 * `context`: { kind: 'invitation' | 'reminder', document, party, url }.
 * Never throws: a failed delivery must not undo the send; the owner still
 * has the link.
 */
async function deliverLink(context) {
  try {
    return await provider.deliver(context);
  } catch (err) {
    logger.warn(`Delivery over ${provider.channel} failed: ${err.message}`);
    return { delivered: false, channel: provider.channel };
  }
}

module.exports = { deliverLink };
