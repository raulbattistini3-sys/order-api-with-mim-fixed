// Each attack is an axios request interceptor. An interceptor sits exactly where
// a man in the middle sits: after the client decided what to send, before it
// hits the wire. It can copy, hold, rewrite or multiply the request.
//
// The attacker never knows CLIENT_SECRET, so it can only replay or mutate what
// it captured. It cannot produce a valid new signature.
import axios from "axios";
import { randomUUID } from "node:crypto";

/**
 * One attack against a captured request.
 *
 * @typedef {object} Attack
 * @property {string} name Short name, printed in the demo's results.
 * @property {string} what What the attack does, in one line.
 * @property {number} copies How many requests it sends.
 * @property {(config: import("axios").InternalAxiosRequestConfig) =>
 *   import("axios").InternalAxiosRequestConfig | Promise<import("axios").InternalAxiosRequestConfig>} interceptor
 *   Copies, holds or rewrites the request. Runs once per copy.
 */

/**
 * A signed request as the man in the middle saw it.
 *
 * @typedef {object} Captured
 * @property {string} method Uppercase HTTP method.
 * @property {string} path Path and query.
 * @property {Record<string, any>} headers All request headers.
 * @property {string} body Raw body, exactly the bytes that were signed.
 */

/**
 * The outcome of one request sent by an attack.
 *
 * @typedef {object} Shot
 * @property {number} status HTTP status, or `0` if no response came back.
 * @property {boolean} [replayed] True when served from the idempotency cache.
 * @property {unknown} [body] Response body.
 * @property {string} [error] Network error message, when `status` is `0`.
 */

/**
 * @param {number} ms
 * @returns {Promise<void>}
 */
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/**
 * Builds the four attacks the demo runs.
 *
 * @param {object} options
 * @param {number} options.replayDelayMs How long late-replay holds its copy.
 *   Must exceed the gateway's `SIGNATURE_MAX_AGE` for it to be rejected.
 * @returns {Attack[]} duplicate-race, key-swap, body-tamper and late-replay.
 */
export function buildAttacks({ replayDelayMs }) {
  return [
    {
      name: "duplicate-race",
      what: "10 identical copies fired at once, racing the original",
      copies: 10,
      interceptor: (config) => config, // byte-for-byte replay
    },
    {
      name: "key-swap",
      what: "fresh Idempotency-Key so the server thinks it's a new order",
      copies: 1,
      interceptor: (config) => {
        config.headers.set("Idempotency-Key", randomUUID());
        return config;
      },
    },
    {
      name: "body-tamper",
      what: "quantity × 100, same key and signature",
      copies: 1,
      interceptor: (config) => {
        const body = JSON.parse(config.data);
        body.quantity *= 100;
        config.data = JSON.stringify(body);
        return config;
      },
    },
    {
      name: "late-replay",
      what: `held for ${replayDelayMs / 1000}s, then replayed unchanged`,
      copies: 1,
      // Interceptors may be async: holding the request is a delay attack.
      interceptor: async (config) => {
        await sleep(replayDelayMs);
        return config;
      },
    },
  ];
}

// Headers worth replaying; hop-by-hop ones would confuse the server.
const REPLAYED_HEADERS = ["content-type", "idempotency-key", "x-timestamp", "x-signature"];

/**
 * Runs one attack against a captured request. Network failures are reported as
 * shots with status `0` rather than thrown.
 *
 * @param {Attack} attack The attack to run.
 * @param {Captured} captured The request to attack.
 * @param {string} target Origin to send the copies to.
 * @returns {Promise<Shot[]>} One shot per copy.
 */
export async function runAttack(attack, captured, target) {
  const client = axios.create({
    baseURL: target,
    timeout: 15_000,
    validateStatus: () => true,
    transformRequest: [(data) => data],
  });
  client.interceptors.request.use(attack.interceptor);

  const headers = {};
  for (const [k, v] of Object.entries(captured.headers)) {
    if (REPLAYED_HEADERS.includes(k.toLowerCase())) headers[k] = v;
  }

  const shots = Array.from({ length: attack.copies }, () =>
    client
      .request({ method: captured.method, url: captured.path, data: captured.body, headers })
      .then((res) => ({ status: res.status, replayed: res.headers["idempotent-replayed"] === "true", body: res.data }))
      .catch((err) => ({ status: 0, error: err.message })),
  );
  return Promise.all(shots);
}
