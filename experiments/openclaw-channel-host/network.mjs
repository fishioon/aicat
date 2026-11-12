import net from 'node:net';
import {syncBuiltinESMExports} from 'node:module';

// Prototype boundary, not a sandbox: deny network outside the explicit fixture endpoint.
export function restrictNetwork(endpoint) {
  const allowed = new URL(endpoint);
  if (!['http:', 'ws:'].includes(allowed.protocol) || allowed.hostname !== '127.0.0.1' || !allowed.port) throw Error('SYNTHETIC_LOOPBACK_ENDPOINT_REQUIRED');
  let attempts = 0;
  const blocked = [];
  const connect = net.Socket.prototype.connect;
  net.Socket.prototype.connect = function (...args) {
    const value = Array.isArray(args[0]) ? args[0][0] : args[0];
    const options = typeof value === 'object' ? value : {port: value, host: typeof args[1] === 'string' ? args[1] : 'localhost'};
    const host = options.host ?? options.hostname ?? 'localhost';
    if (options.path || host !== '127.0.0.1' || String(options.port) !== allowed.port) {
      blocked.push({host, port: options.port});
      throw Error('NETWORK_DENIED: only configured loopback fixture is allowed');
    }
    attempts++;
    return connect.apply(this, args);
  };
  const fetch = globalThis.fetch;
  globalThis.fetch = (input, options) => {
    const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url);
    if (url.hostname !== '127.0.0.1' || url.port !== allowed.port || url.protocol !== 'http:') {
      blocked.push({host: url.hostname, port: url.port});
      throw Error('NETWORK_DENIED: fetch outside fixture');
    }
    return fetch(input, {...options, redirect: 'error'});
  };
  syncBuiltinESMExports();
  return {snapshot: () => ({attempts, blocked})};
}
