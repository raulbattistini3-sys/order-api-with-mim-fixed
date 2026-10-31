// The man in the middle, as an axios request interceptor. Installed on the
// victim's client, it sees each request after it was signed and before it hits
// the wire. It lets the original through and launches every attack alongside it.
import { runAttack } from "./attacks.js";

/**
 * Everything one attack produced.
 *
 * @typedef {object} AttackResult
 * @property {import("./attacks.js").Attack} attack The attack that ran.
 * @property {import("./attacks.js").Shot[]} shots One outcome per copy sent.
 */

/**
 * Creates a man in the middle. Pass `tap` to `createClient` to install it.
 * Each distinct `Idempotency-Key` is attacked once, so `409` retries don't
 * launch the attacks again.
 *
 * @param {object} options
 * @param {string} options.target Origin the attacks are sent to.
 * @param {import("./attacks.js").Attack[]} options.attacks Attacks to launch.
 * @returns {{
 *   tap: (config: import("axios").InternalAxiosRequestConfig) => import("axios").InternalAxiosRequestConfig,
 *   done: () => Promise<AttackResult[]>,
 * }} `tap` is the interceptor; `done` resolves when all attacks have finished.
 */
export function createMitm({ target, attacks }) {
  const seen = new Set();
  /** @type {Promise<AttackResult>[]} */
  const running = [];

  const tap = (config) => {
    const method = config.method.toUpperCase();
    const key = config.headers.get("Idempotency-Key");
    if (method === "GET" || seen.has(key)) return config; // a 409 retry is the same capture
    seen.add(key);

    const captured = { method, path: config.url, headers: config.headers.toJSON(), body: config.data };
    for (const attack of attacks) {
      running.push(runAttack(attack, captured, target).then((shots) => ({ attack, shots })));
    }
    return config;
  };

  return { tap, done: () => Promise.all(running) };
}

/**
 * Groups shots by outcome for display.
 *
 * @param {import("./attacks.js").Shot[]} shots
 * @returns {string} A summary such as `2× 409, 8× 201 (replayed)`.
 */
export function summarize(shots) {
  /** @type {Record<string, number>} */
  const counts = {};
  for (const s of shots) {
    const label = s.status + (s.replayed ? " (replayed)" : "");
    counts[label] = (counts[label] ?? 0) + 1;
  }
  return Object.entries(counts)
    .map(([label, n]) => (n > 1 ? `${n}× ${label}` : label))
    .join(", ");
}
