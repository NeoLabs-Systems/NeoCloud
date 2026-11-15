'use strict';

// A fake SMTP server for tests: service mail goes through nodemailer's own
// message builder into memory instead of the network, so a test reads what
// would have been sent (headers, text, HTML, links) and can make SMTP fail.
//
//   const mail = installFakeMail();          // swaps the transport
//   mail.configure();                        // sets the NEOCLOUD_EMAIL_* env
//   await mail.next();                       // the next message not read yet
//   mail.linkOf(message)                     // the link in the mail's text part
//
// Call after requiring test/helpers/app (it sets the temp NEOCLOUD_HOME first).

const nodemailer = require('nodemailer');
const { setTransportFactoryForTests } = require('../../server/services/account/service_email');

const EMAIL_ENV = [
  'NEOCLOUD_EMAIL_FROM', 'NEOCLOUD_EMAIL_SMTP_HOST', 'NEOCLOUD_EMAIL_SMTP_PORT', 'NEOCLOUD_EMAIL_SMTP_USER',
  'NEOCLOUD_EMAIL_SMTP_PASS', 'NEOCLOUD_EMAIL_PUBLIC_URL', 'NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION',
  'NEOCLOUD_EMAIL_REQUIRE_EMAIL_CHANGE_CONFIRMATION', 'NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN',
  'NEOCLOUD_EMAIL_NOTIFY_ACCOUNT_CHANGES', 'NEOCLOUD_EMAIL_BRAND_NAME', 'NEOCLOUD_EMAIL_REPLY_TO',
  'NEOCLOUD_EMAIL_SIGNING_MAILS', 'NEOCLOUD_EMAIL_SUPPORT_URL',
];

function installFakeMail() {
  const builder = nodemailer.createTransport({ streamTransport: true, buffer: true, newline: 'unix' });
  const sent = [];
  const waiting = [];
  let read = 0;
  let failure = null;

  setTransportFactoryForTests((config) => ({
    async sendMail(payload) {
      if (failure) {
        const error = failure;
        failure = null;
        throw error;
      }
      const info = await builder.sendMail(payload);
      const message = {
        payload,
        config,
        to: [].concat(payload.to),
        subject: payload.subject,
        text: payload.text,
        html: payload.html,
        raw: info.message.toString('utf8'),
      };
      sent.push(message);
      waiting.splice(0).forEach((resolve) => resolve());
      return { messageId: info.messageId };
    },
    close() {},
  }));

  return {
    sent,
    /** Points the server at a "configured" SMTP host (replacing any earlier mail settings); `extra` adds env values. */
    configure(extra = {}) {
      for (const key of EMAIL_ENV) delete process.env[key];
      Object.assign(process.env, {
        NEOCLOUD_EMAIL_FROM: 'NeoCloud <cloud@example.com>',
        NEOCLOUD_EMAIL_SMTP_HOST: 'smtp.test.invalid',
        NEOCLOUD_EMAIL_PUBLIC_URL: 'https://cloud.example.com',
        ...extra,
      });
    },
    /** Back to "no mail configured". */
    reset() {
      for (const key of EMAIL_ENV) delete process.env[key];
      sent.length = 0;
      read = 0;
      failure = null;
    },
    /** The next send throws `error`. */
    failNext(error = Object.assign(new Error('Connection refused'), { code: 'ECONNECTION' })) {
      failure = error;
    },
    /** The next message nobody has read yet (waiting for it), or a rejection after `timeoutMs`. */
    async next({ timeoutMs = 3000 } = {}) {
      const deadline = Date.now() + timeoutMs;
      while (sent.length <= read) {
        if (Date.now() > deadline) throw new Error('No mail arrived in time');
        await new Promise((resolve) => {
          waiting.push(resolve);
          setTimeout(resolve, 25);
        });
      }
      read += 1;
      return sent[read - 1];
    },
    /** Waits a moment and confirms no unread mail turned up (for "no mail" assertions). */
    async quiet(ms = 150) {
      await new Promise((resolve) => setTimeout(resolve, ms));
      return sent.length === read;
    },
    /** Marks everything sent so far as read. */
    drain() {
      read = sent.length;
    },
    /** The first http(s) link in the text part. */
    linkOf(message) {
      return message.text.match(/https?:\/\/\S+/)?.[0] || null;
    },
    /** The token query value of that link. */
    tokenOf(message) {
      return new URL(this.linkOf(message)).searchParams.get('token');
    },
    restore() {
      setTransportFactoryForTests(null);
    },
  };
}

module.exports = { installFakeMail };
