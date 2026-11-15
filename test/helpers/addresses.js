'use strict';

// An app whose every request comes from its own client address (the app
// trusts X-Forwarded-For from loopback), so a test that makes many sign-ins
// or mail requests never meets the per-address rate limits, which it is not
// testing. A test of a limiter itself passes a fixed address.
//
//   const { api, session } = createClients();
//   await api.post('/api/auth/login').send(...)      // no cookies, new address
//   const me = session();                           // cookies kept, new address per call

const { request } = require('./app');
const { createApp } = require('../../server/app');

function createClients() {
  const { app } = createApp({ publicUrl: null, secureCookies: false, trustProxy: ['loopback'] });
  let counter = 0;

  function fresh(req) {
    counter += 1;
    return req.set('X-Forwarded-For', `203.0.${Math.floor(counter / 250)}.${(counter % 250) + 1}`);
  }

  function wrap(target) {
    const call = (method) => (path) => fresh(target[method](path));
    return { get: call('get'), post: call('post'), put: call('put'), delete: call('delete') };
  }

  return {
    app,
    api: wrap(request(app)),
    /** A client that keeps its cookies. */
    session: () => wrap(request.agent(app)),
    fresh,
  };
}

module.exports = { createClients };
