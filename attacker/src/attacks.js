// Each attack is an axios request interceptor. An interceptor sits exactly where
// a man in the middle sits: after the client decided what to send, before it
// hits the wire. It can copy, hold, rewrite or multiply the request.
//
// The attacker never knows CLIENT_SECRET, so it can only replay or mutate what
// it captured. It cannot produce a valid new signature.
import axios from "axios";
import { randomUUID } from "node:crypto";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

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

// Runs one attack against a captured request. Returns the status of every copy.
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
